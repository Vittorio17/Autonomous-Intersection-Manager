package vehicle

import (
    "context"
    "log"
    "time"

    pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

const tickDuration = 1.0

func Simulate(managerAddr string, vehicleID string, speed, distance float64, lane pb.Lane, dir pb.Direction) {
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
	log.Printf("[%s] Partito! Corsia: %v, Dir: %v, Vel: %.1fm/s, Dist: %.1fm",
		vehicleID, lane, dir, kin.Speed, kin.Distance)

    for kin.Distance > 0 {
        eta := kin.EstimateETA()

        req := &pb.VehicleRequest{
            VehicleId:  vehicleID,
            Speed:      kin.Speed,
            Eta:        eta,
            OriginLane: lane,
            Direction:  dir,
        }

        if err := stream.Send(req); err != nil {
            log.Printf("[%s] Errore invio: %v", vehicleID, err)
            return
        }

        res, err := stream.Recv()
        if err != nil {
            log.Printf("[%s] Disconnesso dal server", vehicleID)
            return
        }

        if res.Status == pb.CommandStatus_STATUS_REJECT {
			kin.SetTargetFromETA(res.TargetEta)
			log.Printf("[%s] REJECT! Target ETA: %.2fs -> Rampa verso Vel target: %.2f m/s (attuale: %.2f m/s)",
				vehicleID, res.TargetEta, kin.TargetSpeed, kin.Speed)
		}

		kin.Step(tickDuration)
		time.Sleep(time.Duration(tickDuration * float64(time.Second)))
    }
    
    stream.CloseSend()
    log.Printf("[%s] Incrocio superato con successo!", vehicleID)
}