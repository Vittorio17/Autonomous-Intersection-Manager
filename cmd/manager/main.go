package main

import (
	"fmt"
	"io"
	"log"
	"math"
	"net"
	"sync"

	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	safetyGapSec     = 2.0 // Distanza temporale minima tra due veicoli in conflitto
	commitHorizonSec = 1.5 // Oltre questa soglia il veicolo è troppo vicino per frenare in sicurezza
)

type VehicleState struct {
	VehicleID   string
	Speed       float64
	ETA         float64
	OriginLane  pb.Lane
	Direction   pb.Direction
	IsEmergency bool
	NotifyCh    chan *pb.ManagerResponse // Canale thread-safe per invii sullo stream del veicolo
}

// VehicleRegistry mantiene le prenotazioni attive in memoria
type VehicleRegistry struct {
	mu       sync.Mutex
	vehicles map[string]VehicleState
}

func (r *VehicleRegistry) RemoveVehicle(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.vehicles, id)
}

// ReserveOrSuggest valuta la richiesta, gestisce la preemption per i veicoli di emergenza
// e salva/aggiorna lo stato nel registry in modo atomico.
func (r *VehicleRegistry) ReserveOrSuggest(req *pb.VehicleRequest, notifyCh chan *pb.ManagerResponse) (pb.CommandStatus, float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	targetETA := req.Eta

	if req.IsEmergency {
		// 1. Verifica che nessun veicolo ordinario sia già nella zona di non-ritorno (commitHorizonSec)
		//    o che non ci sia un altro veicolo di emergenza in conflitto.
		for {
			shifted := false
			for id, v := range r.vehicles {
				if id == req.VehicleId {
					continue
				}
				if arePathsConflicting(req.OriginLane, req.Direction, v.OriginLane, v.Direction) {
					if math.Abs(v.ETA-targetETA) < safetyGapSec {
						if v.IsEmergency || v.ETA <= commitHorizonSec {
							targetETA = v.ETA + safetyGapSec
							shifted = true
							break
						}
					}
				}
			}
			if !shifted {
				break
			}
		}

		// 2. Sposta i veicoli ordinari in conflitto (preemption)
		r.preemptConflictingVehicles(req, targetETA)

		// 3. Salva o aggiorna il veicolo di emergenza nel registry
		r.vehicles[req.VehicleId] = VehicleState{
			VehicleID:   req.VehicleId,
			Speed:       req.Speed,
			ETA:         targetETA,
			OriginLane:  req.OriginLane,
			Direction:   req.Direction,
			IsEmergency: true,
			NotifyCh:    notifyCh,
		}

		if math.Abs(targetETA-req.Eta) < 1e-6 {
			return pb.CommandStatus_STATUS_ACK_LOCK, targetETA
		}
		return pb.CommandStatus_STATUS_REJECT, targetETA
	}

	// --- FLUSSO VEICOLO ORDINARIO ---
	for {
		shifted := false
		for id, v := range r.vehicles {
			if id == req.VehicleId {
				continue // Evita auto-conflitto sui tick successivi
			}
			if arePathsConflicting(req.OriginLane, req.Direction, v.OriginLane, v.Direction) {
				if math.Abs(v.ETA-targetETA) < safetyGapSec {
					targetETA = v.ETA + safetyGapSec
					shifted = true
					break
				}
			}
		}
		if !shifted {
			break
		}
	}

	if math.Abs(targetETA-req.Eta) < 1e-6 {
		r.vehicles[req.VehicleId] = VehicleState{
			VehicleID:   req.VehicleId,
			Speed:       req.Speed,
			ETA:         targetETA,
			OriginLane:  req.OriginLane,
			Direction:   req.Direction,
			IsEmergency: false,
			NotifyCh:    notifyCh,
		}
		return pb.CommandStatus_STATUS_ACK_LOCK, targetETA
	}

	return pb.CommandStatus_STATUS_REJECT, targetETA
}

func (r *VehicleRegistry) preemptConflictingVehicles(emergReq *pb.VehicleRequest, emergETA float64) {
	for id, v := range r.vehicles {
		if id == emergReq.VehicleId || v.IsEmergency {
			continue
		}
		if arePathsConflicting(emergReq.OriginLane, emergReq.Direction, v.OriginLane, v.Direction) {
			if math.Abs(v.ETA-emergETA) < safetyGapSec {
				newETA := emergETA + safetyGapSec
				v.ETA = newETA
				r.vehicles[id] = v

				if v.NotifyCh != nil {
					select {
					case v.NotifyCh <- &pb.ManagerResponse{
						VehicleId: v.VehicleID,
						Status:    pb.CommandStatus_STATUS_ADJUST_SPEED,
						TargetEta: newETA,
					}:
					default:
						log.Printf("Canale notifiche pieno per %s, aggiornamento saltato", v.VehicleID)
					}
				}
			}
		}
	}
}

type intersectionServer struct {
	pb.UnimplementedIntersectionServiceServer
	registry *VehicleRegistry
}

func (s *intersectionServer) Negotiate(stream pb.IntersectionService_NegotiateServer) error {
	log.Println("A new vehicle has connected to the intersection!")

	var currentVehicleID string
	defer func() {
		if currentVehicleID != "" {
			s.registry.RemoveVehicle(currentVehicleID)
			log.Printf("Veicolo rimosso dal registro: %s", currentVehicleID)
		}
	}()

	// Canale unico di invio per questo stream (garantisce che stream.Send non sia mai chiamato concorrentemente)
	sendCh := make(chan *pb.ManagerResponse, 16)
	doneCh := make(chan struct{})
	defer close(doneCh)

	go func() {
		for {
			select {
			case msg := <-sendCh:
				if err := stream.Send(msg); err != nil {
					return
				}
			case <-doneCh:
				return
			}
		}
	}()

	for {
		req, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			if status.Code(err) == codes.Canceled {
				return nil
			}
			log.Printf("Error receiving request: %v", err)
			return err
		}

		// Validazione dati di ingresso
		if req.VehicleId == "" || req.Speed < 0 || req.Eta < 0 {
			log.Printf("WARNING: Malformed package discarded. ID='%s', Speed=%.2f, ETA=%.2f", req.VehicleId, req.Speed, req.Eta)
			continue
		}

		currentVehicleID = req.VehicleId

		decision, suggestedETA := s.registry.ReserveOrSuggest(req, sendCh)

		if decision == pb.CommandStatus_STATUS_REJECT {
			log.Printf("ATTENZIONE: Conflitto per %s (ETA: %.2f). Suggerito nuovo ETA: %.2f", req.VehicleId, req.Eta, suggestedETA)
		}

		fmt.Printf(
			"Veichle Received: ID=%s, Speed=%.2f m/s, ETA=%.2f seconds (Emergency=%v)\n",
			req.VehicleId,
			req.Speed,
			req.Eta,
			req.IsEmergency,
		)

		res := &pb.ManagerResponse{
			VehicleId: req.VehicleId,
			Status:    decision,
			TargetEta: suggestedETA,
		}

		select {
		case sendCh <- res:
		case <-doneCh:
			return nil
		}
	}
}

func main() {
	fmt.Println("Running the Intersection Manager...")

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		log.Fatalf("Impossibile aprire la porta: %v", err)
	}

	grpcServer := grpc.NewServer()
	server := &intersectionServer{
		registry: &VehicleRegistry{
			vehicles: make(map[string]VehicleState),
		},
	}

	pb.RegisterIntersectionServiceServer(grpcServer, server)
	log.Println("Intersection Manager listening on port 50051...")

	if err := grpcServer.Serve(lis); err != nil {
		log.Fatalf("Error starting server: %v", err)
	}
}