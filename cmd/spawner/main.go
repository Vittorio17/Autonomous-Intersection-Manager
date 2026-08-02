package main

import (
    "fmt"
    "log"
    "math/rand"
    "os"
    "time"

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
    time.Sleep(2 * time.Second) // Aspetta il Manager
    rand.Seed(time.Now().UnixNano()) // Inizializza il generatore casuale

    managerAddr := os.Getenv("MANAGER_ADDR")
    if managerAddr == "" {
        managerAddr = "localhost:50051"
    }

    log.Println("Traffic Spawner avviato. Generazione traffico in corso...")

    carCount := 1
    for {
        vehicleID := fmt.Sprintf("CAR_%03d", carCount)
        speed := 30.0 + rand.Float64()*20.0      // 30 - 50 m/s
        distance := 300.0 + rand.Float64()*200.0 // 300 - 500 m
        lane := randomLane()
        dir := randomDirection()

        // Lancia il veicolo come Goroutine usando il modulo condiviso
        go vehicle.Simulate(managerAddr, vehicleID, speed, distance, lane, dir)

        carCount++
        time.Sleep(1500 * time.Millisecond) // Auto ogni 1.5 secondi
    }
}