package main

import (
	"context"
	"log"
	"io"
	"os"
	
	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func main() {
	// Legge l'indirizzo, se non c'è usa localhost come fallback per i test senza Docker
	targetAddr := os.Getenv("MANAGER_ADDR")
    if targetAddr == "" {
        targetAddr = "localhost:50051"
    }

    log.Printf("Tentativo di connessione al Manager su %s...\n", targetAddr)

    conn, err := grpc.NewClient(targetAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))

	// 1. Imposta la connessione client gRPC. 
	// Usiamo insecure.NewCredentials() perché per ora non stiamo usando certificati SSL/TLS.
	if err != nil {
		log.Fatalf("Impossibile connettersi al server: %v", err)
	}
	// Garantisce che la connessione di rete venga chiusa quando il programma termina
	defer conn.Close()

	// 2. Inizializza il client generato da Protobuf
	client := pb.NewIntersectionServiceClient(conn)

	// 3. Apre lo stream bidirezionale chiamando Negotiate()
	log.Println("Apertura dello stream bidirezionale in corso...")
	stream, err := client.Negotiate(context.Background())
	if err != nil {
		log.Fatalf("Errore durante l'apertura dello stream: %v", err)
	}

	log.Println("Stream aperto con successo! Connessione stabilita.")

	req := &pb.VehicleRequest{
		VehicleId: "CAR_001",
		Speed:     50.0,
		Eta:       12.4,
	}

	// 2. Inviare la richiesta
	log.Printf("Invio telemetria -> ID: %s | Speed: %.1f m/s | ETA: %.1f s\n", req.VehicleId, req.Speed, req.Eta)
	if err := stream.Send(req); err != nil {
		log.Printf("ERRORE: Fallimento durante l'invio dei dati: %v\n", err)
		return
	}

	// 3. Chiudere la metà in "uscita" dello stream
	// Diciamo al Manager: "Non ho più telemetria da inviare". 
	// Questo scatenerà l'errore `io.EOF` sul server, permettendogli di chiudere il ciclo in modo pulito.
	if err := stream.CloseSend(); err != nil {
		log.Printf("ERRORE durante la chiusura dello stream in uscita: %v\n", err)
	}

	// 4. Leggere la risposta del Manager
	for {
		res, err := stream.Recv()
		if err == io.EOF {
			log.Println("Il Manager ha chiuso lo stream correttamente. Disconnessione.")
			break
		}
		if err != nil {
			log.Printf("ERRORE: Connessione interrotta durante la lettura: %v\n", err)
			break
		}
		// 5. Loggare la risposta a console
		log.Printf("Risposta ricevuta <- Veicolo: %s | Comando: %s\n", res.VehicleId, res.Status)
	}

}