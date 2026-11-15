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

const (
	tickDuration       = 0.2  // 200ms (5 Hz) per telemetria fluida e controllo reattivo
	intersectionWidthM = 14.0 // Larghezza standard dell'incrocio (m)
	carLengthM         = 4.5  // Lunghezza auto standard (m)
	emergencyLengthM   = 6.5  // Lunghezza mezzo di emergenza (m)
)

func Simulate(managerAddr string, vehicleID string, speed, distance float64, lane pb.Lane, dir pb.Direction, isEmergency bool) {
	vehicleLen := carLengthM
	if isEmergency {
		vehicleLen = emergencyLengthM
	}
	exitThreshold := -(intersectionWidthM + vehicleLen)

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
		slog.Float64("vehicle_length_m", vehicleLen),
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

	sendTelemetry := func(exited bool) error {
		eta := 0.0
		if kin.Distance > 0 {
			eta = kin.EstimateETA()
		}
		req := &pb.VehicleRequest{
			VehicleId:      vehicleID,
			Speed:          kin.Speed,
			Eta:            eta,
			OriginLane:     lane,
			Direction:      dir,
			IsEmergency:    isEmergency,
			VehicleLengthM: vehicleLen,
			DistanceM:      kin.Distance,
			HasExited:      exited,
		}
		return stream.Send(req)
	}

	if err := sendTelemetry(false); err != nil {
		vLog.Error("Errore invio telemetria iniziale", slog.Any("error", err))
		return
	}

	ticker := time.NewTicker(time.Duration(tickDuration * float64(time.Second)))
	defer ticker.Stop()

	for kin.Distance > exitThreshold {
		select {
		case res := <-respCh:
			switch res.Status {
			case pb.CommandStatus_STATUS_ACK_LOCK:
				kin.ClearStopTarget()
			case pb.CommandStatus_STATUS_REJECT, pb.CommandStatus_STATUS_ADJUST_SPEED:
				if kin.Distance > 0 {
					if res.TargetEta > 0 {
						kin.SetTargetFromETA(res.TargetEta)
						vLog.Warn("Ricalcolo velocità richiesto",
							slog.String("status", res.Status.String()),
							slog.Float64("assigned_eta", res.TargetEta),
							slog.Float64("target_speed", kin.TargetSpeed),
							slog.Float64("current_speed", kin.Speed),
						)
					} else if res.StopDistanceM >= 0 {
						kin.SetStopTarget(res.StopDistanceM)
					}
				}
			}

		case err := <-errCh:
			if err != io.EOF {
				vLog.Error("Stream interrotto", slog.Any("error", err))
			}
			return

		case <-ticker.C:
			kin.Step(tickDuration)
			if kin.Distance <= exitThreshold {
				break
			}
			if err := sendTelemetry(false); err != nil {
				vLog.Error("Errore invio telemetria", slog.Any("error", err))
				return
			}
		}
	}

	// Segnala esplicitamente al Manager di aver liberato l'incrocio prima di chiudere lo stream
	_ = sendTelemetry(true)
	stream.CloseSend()
	vLog.Info("Incrocio superato e liberato con successo")
}