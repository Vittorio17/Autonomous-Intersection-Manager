package main

import (
	"io"
	"log/slog"
	"math"
	"net"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/Vittorio17/Autonomous-Intersection-Manager/internal/logger"
	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	safetyGapSec     = 2.0
	commitHorizonSec = 1.5
)

type VehicleState struct {
	VehicleID   string
	Speed       float64
	ETA         float64
	OriginLane  pb.Lane
	Direction   pb.Direction
	IsEmergency bool
	NotifyCh    chan *pb.ManagerResponse
}

type VehicleRegistry struct {
	mu       sync.Mutex
	vehicles map[string]VehicleState
	metrics  *Metrics
}

func (r *VehicleRegistry) RemoveVehicle(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.vehicles, id)
}

func (r *VehicleRegistry) ReserveOrSuggest(req *pb.VehicleRequest, notifyCh chan *pb.ManagerResponse) (pb.CommandStatus, float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	targetETA := req.Eta

	if req.IsEmergency {
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

		r.preemptConflictingVehicles(req, targetETA)

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

	for {
		shifted := false
		for id, v := range r.vehicles {
			if id == req.VehicleId {
				continue
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

				if r.metrics != nil {
					r.metrics.RecordPreemption()
				}

				slog.Info("Preemption slot veicolo ordinario",
					slog.String("vehicle_id", v.VehicleID),
					slog.String("preempted_by", emergReq.VehicleId),
					slog.Float64("new_eta", newETA),
				)

				if v.NotifyCh != nil {
					select {
					case v.NotifyCh <- &pb.ManagerResponse{
						VehicleId: v.VehicleID,
						Status:    pb.CommandStatus_STATUS_ADJUST_SPEED,
						TargetEta: newETA,
					}:
					default:
						slog.Warn("Canale notifiche pieno, aggiornamento saltato",
							slog.String("vehicle_id", v.VehicleID),
						)
					}
				}
			}
		}
	}
}

type intersectionServer struct {
	pb.UnimplementedIntersectionServiceServer
	registry *VehicleRegistry
	metrics  *Metrics
}

func (s *intersectionServer) Negotiate(stream pb.IntersectionService_NegotiateServer) error {
	slog.Debug("Nuovo stream gRPC connesso")

	var currentVehicleID string
	completedCleanly := false

	defer func() {
		if currentVehicleID != "" {
			s.registry.RemoveVehicle(currentVehicleID)
			if completedCleanly && s.metrics != nil {
				s.metrics.RecordVehicleCompleted()
			}
			slog.Info("Veicolo rimosso dal registro",
				slog.String("vehicle_id", currentVehicleID),
				slog.Bool("completed", completedCleanly),
			)
		}
	}()

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
			completedCleanly = true
			return nil
		}
		if err != nil {
			if status.Code(err) == codes.Canceled {
				return nil
			}
			slog.Error("Errore ricezione stream", slog.Any("error", err))
			return err
		}

		if req.VehicleId == "" || req.Speed < 0 || req.Eta < 0 {
			slog.Warn("Pacchetto malformato scartato",
				slog.String("vehicle_id", req.VehicleId),
				slog.Float64("speed", req.Speed),
				slog.Float64("eta", req.Eta),
			)
			continue
		}

		currentVehicleID = req.VehicleId
		if s.metrics != nil {
			s.metrics.RecordVehicleSeen(req.VehicleId)
		}

		decision, suggestedETA := s.registry.ReserveOrSuggest(req, sendCh)
		isReject := decision == pb.CommandStatus_STATUS_REJECT

		if s.metrics != nil {
			s.metrics.RecordDecision(isReject)
		}

		logAttrs := []any{
			slog.String("vehicle_id", req.VehicleId),
			slog.String("lane", req.OriginLane.String()),
			slog.String("direction", req.Direction.String()),
			slog.Float64("speed", req.Speed),
			slog.Float64("eta", req.Eta),
			slog.Float64("assigned_eta", suggestedETA),
			slog.String("status", decision.String()),
			slog.Bool("is_emergency", req.IsEmergency),
		}

		if isReject {
			slog.Warn("Conflitto rilevato nella negoziazione", logAttrs...)
		} else {
			slog.Info("Negoziazione elaborata", logAttrs...)
		}

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
	logger.Init("intersection-manager")
	slog.Info("Avvio Intersection Manager...")

	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
		slog.Error("Impossibile aprire la porta", slog.String("port", ":50051"), slog.Any("error", err))
		os.Exit(1)
	}

	metrics := NewMetrics()
	grpcServer := grpc.NewServer()
	server := &intersectionServer{
		metrics: metrics,
		registry: &VehicleRegistry{
			vehicles: make(map[string]VehicleState),
			metrics:  metrics,
		},
	}

	pb.RegisterIntersectionServiceServer(grpcServer, server)

	// Avvia il server gRPC in una goroutine per gestire l'arresto ordinato (SIGINT/SIGTERM)
	go func() {
		slog.Info("Intersection Manager in ascolto", slog.String("address", ":50051"))
		if err := grpcServer.Serve(lis); err != nil {
			slog.Error("Errore server gRPC", slog.Any("error", err))
		}
	}()

	// Attesa segnale di terminazione (Ctrl+C o docker stop)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	sig := <-sigCh

	slog.Info("Segnale di arresto ricevuto, chiusura ordinata in corso...", slog.String("signal", sig.String()))
	grpcServer.GracefulStop()
	metrics.PrintSummary()
}