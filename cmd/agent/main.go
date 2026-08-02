package main

import (
	"context"
	"log"
	"io"
	"os"
	"sync"
	"time"
	"strconv"

	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

//Stato di un auto in movimento
type VehicleSimulator struct {
    mu       sync.Mutex
    ID       string
    Speed    float64 // m/s
    Distance float64 // metri mancanti all'incrocio
}

func main() {
	time.Sleep(2 * time.Second)
	
	// Legge l'indirizzo, se non c'è usa localhost come fallback per i test senza Docker
	targetAddr := os.Getenv("MANAGER_ADDR")
    if targetAddr == "" {
        targetAddr = "localhost:50051"
    }

    log.Printf("Tentativo di connessione al Manager su %s...\n", targetAddr)

    conn, err := grpc.NewClient(targetAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))

	// Imposta la connessione client gRPC. 
	// Usa insecure.NewCredentials() perché per ora non sta usando certificati SSL/TLS.
	if err != nil {
		log.Fatalf("Impossibile connettersi al server: %v", err)
	}
	// Garantisce che la connessione di rete venga chiusa quando il programma termina
	defer conn.Close()

	// Inizializza il client generato da Protobuf
	client := pb.NewIntersectionServiceClient(conn)

	// Apre lo stream bidirezionale chiamando Negotiate()
	log.Println("Apertura dello stream bidirezionale in corso...")
	stream, err := client.Negotiate(context.Background())
	if err != nil {
		log.Fatalf("Errore durante l'apertura dello stream: %v", err)
	}

	log.Println("Stream aperto con successo! Connessione stabilita.")

	// Leggiamo le variabili d'ambiente passate da Docker
	vID := os.Getenv("VEHICLE_ID")
	if vID == "" { 
		vID = "CAR_001" 
	}

	speedStr := os.Getenv("START_SPEED")
	speed := 50.0
	if speedStr != "" { 
		speed, _ = strconv.ParseFloat(speedStr, 64) 
	}

	distStr := os.Getenv("START_DISTANCE")
	dist := 500.0
	if distStr != "" { 
		dist, _ = strconv.ParseFloat(distStr, 64) 
	}

	// Inizializziamo il nostro veicolo con i parametri dinamici
	sim := &VehicleSimulator{
		ID:       vID,
		Speed:    speed,
		Distance: dist,
	}

	waitc := make(chan struct{})

	// goroutine in background
	go func() {
		for {
			res, err := stream.Recv()
			if err == io.EOF {
				close(waitc)
				return
			}
			if err != nil {
				log.Fatalf("Errore ricezione: %v", err)
			}

			if res.Status == pb.CommandStatus_STATUS_REJECT {
				// Blocca il mutex perché sta modificando la velocità,
				// mentre il loop principale la sta leggendo contemporaneamente
				sim.mu.Lock()
				// Riduce la velocità del 20%
				sim.Speed = sim.Speed * 0.8
				// Stampiamo il log
				log.Printf("Frenata d'emergenza! Accesso negato. Nuova velocità: %.2f m/s", sim.Speed)
				sim.mu.Unlock()
			} else if res.Status == pb.CommandStatus_STATUS_ACK_LOCK {
				// Opzionale: Stampiamo un piccolo feedback visivo se va tutto bene
				// log.Println("Semaforo verde confermato dal Manager.")
			}
			}
	}()

	for {
        sim.mu.Lock()
        // Aggiorna la distanza
        sim.Distance -= sim.Speed * 1.0 
        
        if sim.Distance <= 0 {
            sim.mu.Unlock()
            log.Println("Incrocio superato! Disconnessione in corso...")
            
            // Chiude la comunicazione verso il server
            if err := stream.CloseSend(); err != nil {
                log.Printf("Errore chiusura stream: %v", err)
            }
            break
        }

        // Calcola il nuovo ETA (tempo = spazio / velocità)
        eta := sim.Distance / sim.Speed
        
        // Copia i dati estratti per inviarli in modo sicuro fuori dal Mutex
        currentID := sim.ID
        currentSpeed := sim.Speed
        currentDistance := sim.Distance
        sim.mu.Unlock()
        
        // Crea il pacchetto Protobuf con i dati aggiornati
        req := &pb.VehicleRequest{
            VehicleId: currentID,
            Speed:     currentSpeed,
            Eta:       eta,
        }

        // Invia la telemetria al Manager
        if err := stream.Send(req); err != nil {
            log.Fatalf("Errore durante l'invio della telemetria: %v", err)
        }
        
        log.Printf("In movimento... Distanza: %.1fm | Velocità: %.1fm/s | ETA: %.2fs", currentDistance, currentSpeed, eta)

        // Aspetta 1 secondo prima del prossimo ciclo
        time.Sleep(1 * time.Second)
    }

    // Aspetta che la goroutine finisca di processare le ultime risposte
    <-waitc

}