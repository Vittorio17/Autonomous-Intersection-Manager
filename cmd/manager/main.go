package main

import (
		"fmt"
		"io"
		"log"
		"net"
		"sync"
		"math"

		pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
		"google.golang.org/grpc"
)

type VehicleState struct {
	VehicleID string
	Speed     float64
	ETA       float64
	OriginLane pb.Lane
    Direction  pb.Direction
}

// arePathsConflicting restituisce 'true' se le traiettorie si incrociano fisicamente
func arePathsConflicting(l1 pb.Lane, d1 pb.Direction, l2 pb.Lane, d2 pb.Direction) bool {
    // Se provengono dalla stessa corsia, il conflitto temporale significa tamponamento
    if l1 == l2 {
        return true
    }

    // Helper per capire se due corsie sono opposte
    isOpposite := (l1 == pb.Lane_LANE_NORTH && l2 == pb.Lane_LANE_SOUTH) ||
                  (l1 == pb.Lane_LANE_SOUTH && l2 == pb.Lane_LANE_NORTH) ||
                  (l1 == pb.Lane_LANE_EAST && l2 == pb.Lane_LANE_WEST) ||
                  (l1 == pb.Lane_LANE_WEST && l2 == pb.Lane_LANE_EAST)

    if isOpposite {
        // Se sono su corsie opposte ed ENTRAMBE vanno dritte, le traiettorie sono parallele
        if d1 == pb.Direction_DIR_STRAIGHT && d2 == pb.Direction_DIR_STRAIGHT {
            return false
        }
    }

    // Per tutte le altre combinazioni
    return true
}

// Database in memoria
type VehicleRegistry struct {
	mu       sync.Mutex
	vehicles map[string]VehicleState
}

func (r *VehicleRegistry) RemoveVehicle(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.vehicles, id)
}

// ReserveOrSuggest controlla atomicamente la disponibilità dello slot e lo prenota se libero.
// Restituisce (status, targetETA). Se lo slot è libero, restituisce STATUS_ACK_LOCK e prenota il veicolo.
// Se c'è conflitto, restituisce STATUS_REJECT con il nuovo ETA suggerito.
func (r *VehicleRegistry) ReserveOrSuggest(id string, speed float64, newETA float64, lane pb.Lane, dir pb.Direction) (pb.CommandStatus, float64) {
	safetyMargin := 2.0
	r.mu.Lock()
	defer r.mu.Unlock()

	targetETA := newETA
	conflictFound := true

	for conflictFound {
		conflictFound = false
		for _, vehicle := range r.vehicles {
			if arePathsConflicting(lane, dir, vehicle.OriginLane, vehicle.Direction) {
				if math.Abs(vehicle.ETA - targetETA) < safetyMargin {
					conflictFound = true
					targetETA = vehicle.ETA + safetyMargin
					break
				}
			}
		}
	}

	if targetETA == newETA {
		r.vehicles[id] = VehicleState{
			VehicleID:   id,
			Speed:       speed,
			ETA:         targetETA,
			OriginLane:  lane,
			Direction:   dir,
		}
		return pb.CommandStatus_STATUS_ACK_LOCK, targetETA
	}

	return pb.CommandStatus_STATUS_REJECT, targetETA
}

//Questo garantisce che se ci sono metodi in .proto non implementati qui,
//il programma compili ugualmente, restituendo un messaggio NotImplemented
type intersectionServer struct {
	pb.UnimplementedIntersectionServiceServer
	registry *VehicleRegistry
}

//Stream Bidirezionale
func (s *intersectionServer) Negotiate(stream pb.IntersectionService_NegotiateServer) error {
	fmt.Println("A new vehicle has connected to the intersection!")

	var currentVehicleId string
	//closure con defer. Questa scatterà SOLO quando la funzione Negotiate finisce.
	defer func() {
		if currentVehicleId != "" {
			s.registry.RemoveVehicle(currentVehicleId)
			log.Printf("Veicolo rimosso dal registro: %s", currentVehicleId)
		}
	}()
	for{
		req, err := stream.Recv()
		if err == io.EOF{
			return nil
		}
		if err != nil {
			log.Printf("Error receiving request: %v",err)
			return err
		}

		//Validazione dati di ingresso
		if req.VehicleId == "" || req.Speed < 0 || req.Eta < 0 {
			log.Printf("WARNING: Malformed package discarded. ID='%s', Speed=%.2f, ETA=%.2f", req.VehicleId, req.Speed, req.Eta)
			continue // Salta l'invio della risposta e torna a stream.Recv()
		}

		currentVehicleId = req.VehicleId

		decision, suggestedETA := s.registry.ReserveOrSuggest(req.VehicleId, req.Speed, req.Eta, req.OriginLane, req.Direction)

		if decision == pb.CommandStatus_STATUS_REJECT {
			log.Printf("ATTENZIONE: Conflitto per %s (ETA: %.2f). Suggerito nuovo ETA: %.2f", req.VehicleId, req.Eta, suggestedETA)
		}

		fmt.Printf(
			"Veichle Received: ID=%s, Speed=%.2f m/s, ETA=%.2f seconds\n",
			req.VehicleId,
			req.Speed,
			req.Eta,
		)
		//Costruisce la risposta
		res := &pb.ManagerResponse{
			VehicleId: req.VehicleId,
			Status:    decision,
			TargetEta: suggestedETA,
		}
		//Invia la risposta
		if err := stream.Send(res); err != nil {
            log.Printf("Error sending response to %s: %v", req.VehicleId, err)
            return err
        }
	}
}

func main(){
	fmt.Println("Running the Intersection Manager...")

	//Apre la porta 50051 e gestisce eventuali errori
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
    	log.Fatalf("Impossibile aprire la porta: %v", err)
	}	
	//Crea il Server gRPC
	grpcServer := grpc.NewServer()
	server := &intersectionServer{
		registry: &VehicleRegistry{
			vehicles: make(map[string]VehicleState),
		},
	}
	// Registra il servizio IntersectionService
    pb.RegisterIntersectionServiceServer(
        grpcServer,
        server,
    )
	log.Println("Intersection Manager listening on port 50051...")

    if err := grpcServer.Serve(lis); err != nil {
        log.Fatalf("Error starting server: %v", err)
    }
}