package main

import (
		"fmt"
		"io"
		"log"
		"net"
		"sync"
		"math"

		pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
		"google.golang.org/grpc"
)

type VehicleState struct {
	VehicleID string
	Speed     float64
	ETA       float64
}

// Database in memoria
type VehicleRegistry struct {
	mu       sync.Mutex
	vehicles map[string]VehicleState
}

func (r *VehicleRegistry) UpdateVehicle(id string, speed float64, eta float64) {
    r.mu.Lock()
    defer r.mu.Unlock()
    r.vehicles[id] = VehicleState{
        VehicleID: id,
        Speed:     speed,
        ETA:       eta,
    }
}

func (r *VehicleRegistry) RemoveVehicle(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.vehicles,id)
}

//Questo garantisce che se ci sono metodi in .proto non implementati qui,
//il programma compili ugualmente, restituendo un messaggio NotImplemented
type intersectionServer struct {
	pb.UnimplementedIntersectionServiceServer
	registry *VehicleRegistry
}

//Stream Bidirezionale
func (s *intersectionServer) Negotiate(stream pb.IntersectionService_NegotiateServer) error {
	fmt.Println("A new vehicle has connected to the intersection!")

	var currentVehicleId string
	//closure con defer. Questa scatterà SOLO quando la funzione Negotiate finisce.
	defer func() {
		if currentVehicleId != "" {
			s.registry.RemoveVehicle(currentVehicleId)
			log.Printf("Veicolo rimosso dal registro: %s", currentVehicleId)
		}
	}()
	for{
		req, err := stream.Recv()
		if err == io.EOF{
			return nil
		}
		if err != nil {
			log.Printf("Error receiving request: %v",err)
			return err
		}

		//Validazione dati di ingresso
		if req.VehicleId == "" || req.Speed < 0 || req.Eta < 0 {
			log.Printf("WARNING: Malformed package discarded. ID='%s', Speed=%.2f, ETA=%.2f", req.VehicleId, req.Speed, req.Eta)
			continue // Salta l'invio della risposta e torna a stream.Recv()
		}

		currentVehicleId = req.VehicleId

		var decision pb.CommandStatus

		if(s.registry.HasConflict(req.VeihcleId,req.Eta)){
			decision = pb.CommandStatus_STATUS_REJECT
            log.Printf("ATTENZIONE: Conflitto rilevato per %s (ETA: %.2f). Rifiutato!", req.VehicleId, req.Eta)
		}else{
			decision = pb.CommandStatus_STATUS_ACK_LOCK
		}
		s.registry.UpdateVehicle(req.VehicleId, req.Speed, req.Eta)
		log.Printf("Registro aggiornato per: %s", req.VehicleId)

		fmt.Printf(
			"Veichle Received: ID=%s, Speed=%.2f m/s, ETA=%.2f seconds\n",
			req.VehicleId,
			req.Speed,
			req.Eta,
		)
		//Costruisce la risposta
		res := &pb.ManagerResponse{
			VehicleId: req.VehicleId,
			Status:    decision,
		}
		//Invia la risposta
		if err := stream.Send(res); err != nil {
            log.Printf("Error sending response to %s: %v", req.VehicleId, err)
            return err
        }
	}
}

func main(){
	fmt.Println("Running the Intersection Manager...")

	//Apre la porta 50051 e gestisce eventuali errori
	lis, err := net.Listen("tcp", ":50051")
	if err != nil {
    	log.Fatalf("Impossibile aprire la porta: %v", err)
	}	
	//Crea il Server gRPC
	grpcServer := grpc.NewServer()
	server := &intersectionServer{
		registry: &VehicleRegistry{
			vehicles: make(map[string]VehicleState),
		},
	}
	// Registra il servizio IntersectionService
    pb.RegisterIntersectionServiceServer(
        grpcServer,
        server,
    )
	log.Println("Intersection Manager listening on port 50051...")

    if err := grpcServer.Serve(lis); err != nil {
        log.Fatalf("Error starting server: %v", err)
    }
}

// Controlla se c'è un'auto con un ETA troppo vicino (meno di 2 secondi di differenza)
func (r *VehicleRegistry) HasConflict(currentID string, newETA float64) bool {
    safetyMargin := 2.0
	r.mu.Lock()
    defer r.mu.Unlock()
    
    for id,vehicle := range r.vehicles {
		if (id==currentID) {continue}
		diff := math.Abs(vehicle.ETA-newETA)
		if(diff<safetyMargin){
			//C'è una collisione
			return true
		}
	}
	return false
}