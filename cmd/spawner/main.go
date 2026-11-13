package main

import (
	"fmt"
	"log/slog"
	"math/rand"
	"os"
	"time"

	"github.com/Vittorio17/Autonomous-Intersection-Manager/internal/logger"
	"github.com/Vittorio17/Autonomous-Intersection-Manager/internal/vehicle"
	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
)

func randomLane() pb.Lane {
	lanes := []pb.Lane{pb.Lane_LANE_NORTH, pb.Lane_LANE_SOUTH, pb.Lane_LANE_EAST, pb.Lane_LANE_WEST}
	return lanes[rand.Intn(len(lanes))]
}

func randomDirection() pb.Direction {
	dirs := []pb.Direction{pb.Direction_DIR_STRAIGHT, pb.Direction_DIR_LEFT, pb.Direction_DIR_RIGHT}
	return dirs[rand.Intn(len(dirs))]
}

func main() {
	logger.Init("traffic-spawner")
	time.Sleep(2 * time.Second) // Aspetta il Manager

	managerAddr := os.Getenv("MANAGER_ADDR")
	if managerAddr == "" {
		managerAddr = "localhost:50051"
	}

	slog.Info("Traffic Spawner avviato, generazione traffico in corso",
		slog.String("manager_addr", managerAddr),
	)

	carCount := 1
	for {
		isEmergency := rand.Float64() < 0.10 // 10% di probabilità di veicolo di emergenza

		prefix := "CAR"
		if isEmergency {
			prefix = "AMB"
		}
		vehicleID := fmt.Sprintf("%s_%03d", prefix, carCount)

		speed := 30.0 + rand.Float64()*20.0      // 30 - 50 m/s
		distance := 300.0 + rand.Float64()*200.0 // 300 - 500 m
		lane := randomLane()
		dir := randomDirection()

		// Lancia il veicolo come Goroutine usando il modulo condiviso
		go vehicle.Simulate(managerAddr, vehicleID, speed, distance, lane, dir, isEmergency)

		carCount++
		time.Sleep(1500 * time.Millisecond) // Auto ogni 1.5 secondi
	}
}