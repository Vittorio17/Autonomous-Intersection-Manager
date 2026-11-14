package main

import (
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Vittorio17/Autonomous-Intersection-Manager/internal/logger"
	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type intersectionServer struct {
	pb.UnimplementedIntersectionServiceServer
	grid *ReservationGrid
	metrics  *Metrics
}

func (s *intersectionServer) Negotiate(stream pb.IntersectionService_NegotiateServer) error {
	slog.Debug("Nuovo stream gRPC connesso")

	var currentVehicleID string
	completedCleanly := false

	defer func() {
		if currentVehicleID != "" {
			s.grid.RemoveVehicle(currentVehicleID)
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

		if req.HasExited{
			s.grid.RemoveVehicle(req.VehicleId)
			completedCleanly = true
			return nil
		}

		if s.metrics != nil {
			s.metrics.RecordVehicleSeen(req.VehicleId)
		}

		serverNowMs := time.Now().UnixMilli()
		decision, suggestedETA, entryMs, exitMs := s.grid.ReserveOrSuggest(
			req,
			serverNowMs,
			sendCh,
			func(preemptedID string, newETA float64) {
				if s.metrics != nil {
					s.metrics.RecordPreemption()
				}
				slog.Info("Preemption slot veicolo ordinario",
					slog.String("vehicle_id", preemptedID),
					slog.String("preempted_by", req.VehicleId),
					slog.Float64("new_eta", newETA),
				)
			},
		)

		isReject := decision == pb.CommandStatus_STATUS_REJECT
		if s.metrics != nil {
			s.metrics.RecordDecision(isReject)
		}

		logAttrs := []any{
			slog.String("vehicle_id", req.VehicleId),
			slog.String("lane", req.OriginLane.String()),
			slog.String("direction", req.Direction.String()),
			slog.Float64("distance_m", req.DistanceM),
			slog.Float64("speed", req.Speed),
			slog.Float64("eta", req.Eta),
			slog.Float64("assigned_eta", suggestedETA),
			slog.Int64("entry_time_ms", entryMs),
			slog.Int64("exit_time_ms", exitMs),
			slog.String("status", decision.String()),
			slog.Bool("is_emergency", req.IsEmergency),
		}

		if isReject {
			slog.Warn("Conflitto rilevato nella ReservationGrid", logAttrs...)
		} else {
			slog.Info("Prenotazione confermata", logAttrs...)
		}

		res := &pb.ManagerResponse{
			VehicleId:    req.VehicleId,
			Status:       decision,
			TargetEta:    suggestedETA,
			EntryTimeMs:  entryMs,
			ExitTimeMs:   exitMs,
			ServerTimeMs: serverNowMs,
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
	    grid:    NewReservationGrid(),
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