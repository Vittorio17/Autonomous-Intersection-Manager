package main

import (
    "context"
    "net"
    "testing"
    "time"

    pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
    "google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024
var lis *bufconn.Listener

// init() viene eseguita automaticamente prima dei test
func init() {
    lis = bufconn.Listen(bufSize)
    s := grpc.NewServer()
    // Registriamo il server (intersectionServer definito in main.go)
    pb.RegisterIntersectionServiceServer(s, &intersectionServer{})
    
    // Avviamo il server in una Goroutine separata (in background)
    go func() {
        if err := s.Serve(lis); err != nil {
            panic("Server fallito durante il test: " + err.Error())
        }
    }()
}

// bufDialer dice al client gRPC di usare il "cavo virtuale" in RAM
func bufDialer(context.Context, string) (net.Conn, error) {
    return lis.Dial()
}

func TestEndToEndNegotiation(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()

    // 1. Connessione del client in-memory
    conn, err := grpc.NewClient("passthrough://bufnet", 
        grpc.WithContextDialer(bufDialer), 
        grpc.WithTransportCredentials(insecure.NewCredentials()))
    
    if err != nil {
        t.Fatalf("Impossibile connettersi al bufnet: %v", err)
    }
    defer conn.Close()

    client := pb.NewIntersectionServiceClient(conn)

    // 2. Apertura stream
    stream, err := client.Negotiate(ctx)
    if err != nil {
        t.Fatalf("Fallimento apertura stream: %v", err)
    }

    // 3. Invio richiesta
    req := &pb.VehicleRequest{
        VehicleId: "TEST_CAR_01",
        Speed:     30.0,
        Eta:       5.5,
    }
    if err := stream.Send(req); err != nil {
        t.Fatalf("Impossibile inviare la richiesta: %v", err)
    }

    // 4. Lettura risposta
    res, err := stream.Recv()
    if err != nil {
        t.Fatalf("Impossibile ricevere la risposta: %v", err)
    }

    // 5. ASSERT: Verifica che lo status sia corretto
    if res.Status != pb.CommandStatus_STATUS_ACK_LOCK {
        t.Errorf("TEST FALLITO: Aspettato %v, Ricevuto %v", pb.CommandStatus_STATUS_ACK_LOCK, res.Status)
    }

    if res.VehicleId != req.VehicleId {
        t.Errorf("TEST FALLITO: L'ID veicolo non corrisponde. Inviato %s, ricevuto %s", req.VehicleId, res.VehicleId)
    }
}