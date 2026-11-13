package vehicle

import (
	"context"
	"io"
	"log"
	"time"

	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const tickDuration = 1.0 // secondi per tick di simulazione

func Simulate(managerAddr string, vehicleID string, speed, distance float64, lane pb.Lane, dir pb.Direction, isEmergency bool) {
	conn, err := grpc.Dial(managerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("[%s] Errore di connessione: %v", vehicleID, err)
		return
	}
	defer conn.Close()

	client := pb.NewIntersectionServiceClient(conn)
	stream, err := client.Negotiate(context.Background())
	if err != nil {
		log.Printf("[%s] Errore apertura stream: %v", vehicleID, err)
		return
	}

	kin := NewKinematics(distance, speed, DefaultMaxAccel, DefaultMaxDecel)
	log.Printf("[%s] Partito! Corsia: %v, Dir: %v, Vel: %.1fm/s, Dist: %.1fm, Emergency: %v",
		vehicleID, lane, dir, kin.Speed, kin.Distance, isEmergency)

	respCh := make(chan *pb.ManagerResponse, 16)
	errCh := make(chan error, 1)

	// Goroutine dedicata all'ascolto continuo dal Manager (risposte dirette + push STATUS_ADJUST_SPEED)
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

	// Funzione helper per inviare la telemetria corrente
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

	// Primo invio immediato all'avvio
	if err := sendTelemetry(); err != nil {
		log.Printf("[%s] Errore invio iniziale: %v", vehicleID, err)
		return
	}

	ticker := time.NewTicker(time.Duration(tickDuration * float64(time.Second)))
	defer ticker.Stop()

	for kin.Distance > 0 {
		select {
		case res := <-respCh:
			if res.Status == pb.CommandStatus_STATUS_REJECT || res.Status == pb.CommandStatus_STATUS_ADJUST_SPEED {
				kin.SetTargetFromETA(res.TargetEta)
				log.Printf("[%s] %v! Target ETA: %.2fs -> Rampa verso Vel target: %.2f m/s (attuale: %.2f m/s)",
					vehicleID, res.Status, res.TargetEta, kin.TargetSpeed, kin.Speed)
			}

		case err := <-errCh:
			if err != io.EOF {
				log.Printf("[%s] Disconnesso dal server: %v", vehicleID, err)
			}
			return

		case <-ticker.C:
			kin.Step(tickDuration)
			if kin.Distance <= 0 {
				break
			}
			if err := sendTelemetry(); err != nil {
				log.Printf("[%s] Errore invio telemetria: %v", vehicleID, err)
				return
			}
		}
	}

	stream.CloseSend()
	log.Printf("[%s] Incrocio superato con successo!", vehicleID)
}