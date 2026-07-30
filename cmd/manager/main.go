package main

import (
		"fmt"
		"io"
		"log"
		"net"
		pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
		"google.golang.org/grpc"
)

//Questo garantisce che se ci sono metodi in .proto non implementati qui,
//il programma compili ugualmente, restituendo un messaggio NotImplemented
type intersectionServer struct {
	pb.UnimplementedIntersectionServiceServer
}

//Stream Bidirezionale
func (s *intersectionServer) Negotiate(stream pb.IntersectionService_NegotiateServer) error {
	fmt.Println("A new vehicle has connected to the intersection!")

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

		fmt.Printf(
			"Veichle Received: ID=%s, Speed=%.2f m/s, ETA=%.2f seconds\n",
			req.VehicleId,
			req.Speed,
			req.Eta,
		)
		//Costruisce la risposta
		response := &pb.ManagerResponse{
			VehicleId: req.VehicleId,
			Status:    pb.CommandStatus_STATUS_ACK_LOCK,
		}
		//Invia la risposta
		if err := stream.Send(response); err != nil {
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
	// Registra il servizio IntersectionService
    pb.RegisterIntersectionServiceServer(
        grpcServer,
        &intersectionServer{},
    )
	log.Println("Intersection Manager listening on port 50051...")

    if err := grpcServer.Serve(lis); err != nil {
        log.Fatalf("Error starting server: %v", err)
    }
}