package main

import (
    "context"
    "fmt"
    "net"
    "testing"
    "time"
    "sync"

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
    pb.RegisterIntersectionServiceServer(s, &intersectionServer{
		registry: &VehicleRegistry{
			vehicles: make(map[string]VehicleState),
		},
	})
    
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

func TestReserveOrSuggestConcurrentSameSlot(t *testing.T) {
	registry := &VehicleRegistry{
		vehicles: make(map[string]VehicleState),
	}

	const numGoroutines = 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	results := make(chan struct {
		id     string
		status pb.CommandStatus
		eta    float64
	}, numGoroutines)

	// Tutti i goroutine tentano di prenotare lo stesso ETA (10.0) sulla stessa corsia
	for i := 0; i < numGoroutines; i++ {
		go func(n int) {
			defer wg.Done()
			id := fmt.Sprintf("CAR_%d", n)
			status, eta := registry.ReserveOrSuggest(id, 30.0, 10.0, pb.Lane_LANE_NORTH, pb.Direction_DIR_STRAIGHT)
			results <- struct {
				id     string
				status pb.CommandStatus
				eta    float64
			}{id, status, eta}
		}(i)
	}

	wg.Wait()
	close(results)

	ackCount := 0
	rejectCount := 0
	for r := range results {
		if r.status == pb.CommandStatus_STATUS_ACK_LOCK {
			ackCount++
		} else if r.status == pb.CommandStatus_STATUS_REJECT {
			rejectCount++
		}
	}

	// Solo UNO deve ricevere ACK_LOCK, tutti gli altri REJECT
	if ackCount != 1 {
		t.Errorf("Esattamente un veicolo deve ricevere ACK_LOCK, ne hanno ricevuti %d", ackCount)
	}
	if rejectCount != numGoroutines-1 {
		t.Errorf("%d veicoli devono ricevere REJECT, ne hanno ricevuti %d", numGoroutines-1, rejectCount)
	}
}

func TestConcurrentNegotiation(t *testing.T) {
    // 1. Avvia il server su una porta di test
    lis, err := net.Listen("tcp", ":50052")
    if err != nil {
        t.Fatalf("Impossibile aprire la porta: %v", err)
    }
    
    grpcServer := grpc.NewServer()
    server := &intersectionServer{
        registry: &VehicleRegistry{
            vehicles: make(map[string]VehicleState),
        },
    }
    pb.RegisterIntersectionServiceServer(grpcServer, server)
    
    go func() {
        grpcServer.Serve(lis)
    }()
    defer grpcServer.Stop()

    // Aspetta che il server sia pronto
    time.Sleep(100 * time.Millisecond)

    // 2. Connetti due client simultanei
    conn, err := grpc.NewClient("localhost:50052", grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        t.Fatalf("Errore di connessione: %v", err)
    }
    defer conn.Close()

    client := pb.NewIntersectionServiceClient(conn)

    var wg sync.WaitGroup
    wg.Add(2)

    // Variabili per raccogliere le risposte
    var status1, status2 pb.CommandStatus

    // 3. Lancia CAR_1
    go func() {
        defer wg.Done()
        stream, _ := client.Negotiate(context.Background())
        stream.Send(&pb.VehicleRequest{VehicleId: "CAR_1", Speed: 50.0, Eta: 10.0})
        res, _ := stream.Recv()
        status1 = res.Status
        stream.CloseSend()
    }()

    // 4. Lancia CAR_2 nello stesso istante (stesso ETA!)
    go func() {
        defer wg.Done()
        stream, _ := client.Negotiate(context.Background())
        stream.Send(&pb.VehicleRequest{VehicleId: "CAR_2", Speed: 40.0, Eta: 10.0})
        res, _ := stream.Recv()
        status2 = res.Status
        stream.CloseSend()
    }()

    // Aspetta che entrambe abbiano finito
    wg.Wait()

    // 5. Verifica che una sia passata e una sia stata respinta!
    if status1 == status2 {
        t.Errorf("Entrambe le auto hanno ricevuto lo stesso stato (%v)! La logica anti-collisione ha fallito.", status1)
    } else {
        t.Logf("Successo! Auto 1 ha ricevuto: %v, Auto 2 ha ricevuto: %v", status1, status2)
    }
}