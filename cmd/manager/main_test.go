package main

import (
    "context"
    "fmt"
    "net"
    "sync"
    "testing"
    "time"

    pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure"
    "google.golang.org/grpc/test/bufconn"
)

const bufSize = 1024 * 1024

var lis *bufconn.Listener

func init() {
    lis = bufconn.Listen(bufSize)
    s := grpc.NewServer()
    pb.RegisterIntersectionServiceServer(s, &intersectionServer{
        grid: NewReservationGrid(),
    })

    go func() {
        if err := s.Serve(lis); err != nil {
            panic("Server fallito durante il test: " + err.Error())
        }
    }()
}

func bufDialer(context.Context, string) (net.Conn, error) {
    return lis.Dial()
}

func TestEndToEndNegotiation(t *testing.T) {
    ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
    defer cancel()

    conn, err := grpc.NewClient("passthrough://bufnet",
        grpc.WithContextDialer(bufDialer),
        grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        t.Fatalf("Impossibile connettersi al bufnet: %v", err)
    }
    defer conn.Close()

    client := pb.NewIntersectionServiceClient(conn)

    stream, err := client.Negotiate(ctx)
    if err != nil {
        t.Fatalf("Fallimento apertura stream: %v", err)
    }

    req := &pb.VehicleRequest{
        VehicleId: "TEST_CAR_01",
        Speed:     30.0,
        Eta:       5.5,
    }
    if err := stream.Send(req); err != nil {
        t.Fatalf("Impossibile inviare la richiesta: %v", err)
    }

    res, err := stream.Recv()
    if err != nil {
        t.Fatalf("Impossibile ricevere la risposta: %v", err)
    }

    if res.Status != pb.CommandStatus_STATUS_ACK_LOCK {
        t.Errorf("TEST FALLITO: Aspettato %v, Ricevuto %v", pb.CommandStatus_STATUS_ACK_LOCK, res.Status)
    }

    if res.VehicleId != req.VehicleId {
        t.Errorf("TEST FALLITO: L'ID veicolo non corrisponde. Inviato %s, ricevuto %s", req.VehicleId, res.VehicleId)
    }

    if res.ExitTimeMs <= res.EntryTimeMs {
        t.Errorf("TEST FALLITO: ExitTimeMs (%d) deve essere maggiore di EntryTimeMs (%d)", res.ExitTimeMs, res.EntryTimeMs)
    }
}

func TestReserveOrSuggestConcurrentSameSlot(t *testing.T) {
    grid := NewReservationGrid()
    now := time.Now().UnixMilli()

    const numGoroutines = 10
    var wg sync.WaitGroup
    wg.Add(numGoroutines)

    results := make(chan struct {
        id     string
        status pb.CommandStatus
        eta    float64
    }, numGoroutines)

    for i := 0; i < numGoroutines; i++ {
        go func(n int) {
            defer wg.Done()
            id := fmt.Sprintf("CAR_%d", n)
            req := &pb.VehicleRequest{
                VehicleId:  id,
                Speed:      30.0,
                Eta:        10.0,
                OriginLane: pb.Lane_LANE_NORTH,
                Direction:  pb.Direction_DIR_STRAIGHT,
            }
            status, eta, _, _ := grid.ReserveOrSuggest(req, now, nil, nil)
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

    if ackCount != 1 {
        t.Errorf("Esattamente un veicolo deve ricevere ACK_LOCK, ne hanno ricevuti %d", ackCount)
    }
    if rejectCount != numGoroutines-1 {
        t.Errorf("%d veicoli devono ricevere REJECT, ne hanno ricevuti %d", numGoroutines-1, rejectCount)
    }
}

func TestConcurrentNegotiation(t *testing.T) {
    lis, err := net.Listen("tcp", "127.0.0.1:0")
    if err != nil {
        t.Fatalf("Impossibile aprire la porta: %v", err)
    }

    grpcServer := grpc.NewServer()
    server := &intersectionServer{
        grid: NewReservationGrid(),
    }
    pb.RegisterIntersectionServiceServer(grpcServer, server)

    go func() {
        _ = grpcServer.Serve(lis)
    }()
    defer grpcServer.Stop()

    conn, err := grpc.NewClient(lis.Addr().String(), grpc.WithTransportCredentials(insecure.NewCredentials()))
    if err != nil {
        t.Fatalf("Errore di connessione: %v", err)
    }
    defer conn.Close()

    client := pb.NewIntersectionServiceClient(conn)

    var wg sync.WaitGroup
    wg.Add(2)

    var status1, status2 pb.CommandStatus

    go func() {
        defer wg.Done()
        stream, _ := client.Negotiate(context.Background())
        _ = stream.Send(&pb.VehicleRequest{VehicleId: "CAR_1", Speed: 50.0, Eta: 10.0})
        res, _ := stream.Recv()
        status1 = res.Status
        _ = stream.CloseSend()
    }()

    go func() {
        defer wg.Done()
        stream, _ := client.Negotiate(context.Background())
        _ = stream.Send(&pb.VehicleRequest{VehicleId: "CAR_2", Speed: 40.0, Eta: 10.0})
        res, _ := stream.Recv()
        status2 = res.Status
        _ = stream.CloseSend()
    }()

    wg.Wait()

    if status1 == status2 {
        t.Errorf("Entrambe le auto hanno ricevuto lo stesso stato (%v)! La logica anti-collisione ha fallito.", status1)
    } else {
        t.Logf("Successo! Auto 1 ha ricevuto: %v, Auto 2 ha ricevuto: %v", status1, status2)
    }
}

func TestMultiTickSameVehicleNoSelfConflict(t *testing.T) {
    grid := NewReservationGrid()
    baseNow := int64(1_000_000)

    // Simula lo stesso veicolo su 3 tick consecutivi mentre si avvicina (t=0s, t=1s, t=2s)
    ticks := []struct {
        elapsedMs int64
        eta       float64
    }{
        {0, 6.0},
        {1000, 5.0},
        {2000, 4.0},
    }

    for i, tk := range ticks {
        req := &pb.VehicleRequest{
            VehicleId:  "CAR_MULTITICK",
            Speed:      20.0,
            Eta:        tk.eta,
            OriginLane: pb.Lane_LANE_NORTH,
            Direction:  pb.Direction_DIR_STRAIGHT,
        }
        status, targetETA, _, _ := grid.ReserveOrSuggest(req, baseNow+tk.elapsedMs, nil, nil)
        if status != pb.CommandStatus_STATUS_ACK_LOCK {
            t.Fatalf("Tick %d (ETA %.1f): atteso ACK_LOCK, ottenuto %v (suggerito %.1f)", i, tk.eta, status, targetETA)
        }
    }
}

func TestEmergencyVehiclePreemption(t *testing.T) {
    grid := NewReservationGrid()
    now := int64(2_000_000)
    ordinaryCh := make(chan *pb.ManagerResponse, 4)

    // 1. Un veicolo ordinario prenota lo slot a ETA = 5.0s da Nord -> Dritto
    ordReq := &pb.VehicleRequest{
        VehicleId:   "ORDINARY_01",
        Speed:       20.0,
        Eta:         5.0,
        OriginLane:  pb.Lane_LANE_NORTH,
        Direction:   pb.Direction_DIR_STRAIGHT,
        IsEmergency: false,
    }
    status, _, _, _ := grid.ReserveOrSuggest(ordReq, now, ordinaryCh, nil)
    if status != pb.CommandStatus_STATUS_ACK_LOCK {
        t.Fatalf("Veicolo ordinario doveva ottenere ACK_LOCK, ottenuto %v", status)
    }

    // 2. Arriva un'ambulanza da Est -> Dritto (traiettoria in conflitto) con lo stesso ETA = 5.0s
    ambReq := &pb.VehicleRequest{
        VehicleId:   "AMBULANCE_01",
        Speed:       30.0,
        Eta:         5.0,
        OriginLane:  pb.Lane_LANE_EAST,
        Direction:   pb.Direction_DIR_STRAIGHT,
        IsEmergency: true,
    }
    ambStatus, ambETA, _, ambExitMs := grid.ReserveOrSuggest(ambReq, now, nil, nil)
    if ambStatus != pb.CommandStatus_STATUS_ACK_LOCK || ambETA != 5.0 {
        t.Fatalf("Ambulanza doveva ottenere ACK_LOCK immediato a 5.0s, ottenuto %v (ETA %.1f)", ambStatus, ambETA)
    }

    // 3. Verifica che il veicolo ordinario abbia ricevuto la notifica push STATUS_ADJUST_SPEED
    // dopo l'uscita dell'ambulanza (ambExitMs + buffer dinamico)
    select {
    case pushMsg := <-ordinaryCh:
        if pushMsg.Status != pb.CommandStatus_STATUS_ADJUST_SPEED {
            t.Errorf("Atteso STATUS_ADJUST_SPEED per il veicolo ordinario, ottenuto %v", pushMsg.Status)
        }
        if pushMsg.EntryTimeMs <= ambExitMs {
            t.Errorf("Atteso nuovo EntryTimeMs (%d) > ambExitMs (%d)", pushMsg.EntryTimeMs, ambExitMs)
        }
    default:
        t.Fatal("Il veicolo ordinario non ha ricevuto nessuna notifica di riallocazione slot")
    }
}