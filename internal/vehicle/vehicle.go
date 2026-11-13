package vehicle

import (
	"context"
	"io"
	"log/slog"
	"time"

	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const tickDuration = 1.0

func Simulate(managerAddr string, vehicleID string, speed, distance float64, lane pb.Lane, dir pb.Direction, isEmergency bool) {
	vLog := slog.With(
		slog.String("vehicle_id", vehicleID),
		slog.String("lane", lane.String()),
		slog.String("direction", dir.String()),
		slog.Bool("is_emergency", isEmergency),
	)

	conn, err := grpc.Dial(managerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		vLog.Error("Errore di connessione al manager", slog.Any("error", err))
		return
	}
	defer conn.Close()

	client := pb.NewIntersectionServiceClient(conn)
	stream, err := client.Negotiate(context.Background())
	if err != nil {
		vLog.Error("Errore apertura stream", slog.Any("error", err))
		return
	}

	kin := NewKinematics(distance, speed, DefaultMaxAccel, DefaultMaxDecel)
	vLog.Info("Veicolo avviato",
		slog.Float64("speed", kin.Speed),
		slog.Float64("distance", kin.Distance),
	)

	respCh := make(chan *pb.ManagerResponse, 16)
	errCh := make(chan error, 1)

	go func() {
		for {
			res, err := stream.Recv()
			if err != nil {
				errCh <- err
				return
			}
			respCh <- res
		}
	}()

	sendTelemetry := func() error {
		req := &pb.VehicleRequest{
			VehicleId:   vehicleID,
			Speed:       kin.Speed,
			Eta:         kin.EstimateETA(),
			OriginLane:  lane,
			Direction:   dir,
			IsEmergency: isEmergency,
		}
		return stream.Send(req)
	}

	if err := sendTelemetry(); err != nil {
		vLog.Error("Errore invio telemetria iniziale", slog.Any("error", err))
		return
	}

	ticker := time.NewTicker(time.Duration(tickDuration * float64(time.Second)))
	defer ticker.Stop()

	for kin.Distance > 0 {
		select {
		case res := <-respCh:
			if res.Status == pb.CommandStatus_STATUS_REJECT || res.Status == pb.CommandStatus_STATUS_ADJUST_SPEED {
				kin.SetTargetFromETA(res.TargetEta)
				vLog.Warn("Ricalcolo velocità richiesto",
					slog.String("status", res.Status.String()),
					slog.Float64("assigned_eta", res.TargetEta),
					slog.Float64("target_speed", kin.TargetSpeed),
					slog.Float64("current_speed", kin.Speed),
				)
			}

		case err := <-errCh:
			if err != io.EOF {
				vLog.Error("Stream interrotto", slog.Any("error", err))
			}
			return

		case <-ticker.C:
			kin.Step(tickDuration)
			if kin.Distance <= 0 {
				break
			}
			if err := sendTelemetry(); err != nil {
				vLog.Error("Errore invio telemetria", slog.Any("error", err))
				return
			}
		}
	}

	stream.CloseSend()
	vLog.Info("Incrocio superato con successo")
}