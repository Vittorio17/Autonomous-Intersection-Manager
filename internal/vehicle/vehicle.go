package vehicle

import (
    "context"
    "log"
    "time"

    pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
)

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

    log.Printf("[%s] Partito! Corsia: %v, Dir: %v, Vel: %.0fm/s", vehicleID, lane, dir, speed)

    for distance > 0 {
        eta := distance / speed

        req := &pb.VehicleRequest{
            VehicleId:  vehicleID,
            Speed:      speed,
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
            // Calcoliamo la velocità esatta per arrivare nel time slot suggerito dal Manager.
            speed = distance / res.TargetEta 
            log.Printf("[%s] REJECT! Ricalcolo analitico: Nuovo ETA %.2fs -> Vel esatta: %.2f m/s", vehicleID, res.TargetEta, speed)
        }
        distance -= speed
        time.Sleep(1 * time.Second)
    }
    
    stream.CloseSend()
    log.Printf("[%s] Incrocio superato con successo!", vehicleID)
}