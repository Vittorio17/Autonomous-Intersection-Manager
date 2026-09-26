package main

import (
    "os"
    "strconv"
    "time"

    "github.com/Vittorio17/Autonomous-Intersection-Manager/internal/vehicle"
    pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
)

func parseLane(laneStr string) pb.Lane {
    switch laneStr {
    case "NORTH": return pb.Lane_LANE_NORTH
    case "SOUTH": return pb.Lane_LANE_SOUTH
    case "EAST":  return pb.Lane_LANE_EAST
    case "WEST":  return pb.Lane_LANE_WEST
    default:      return pb.Lane_LANE_UNKNOWN
    }
}

func parseDirection(dirStr string) pb.Direction {
    switch dirStr {
    case "STRAIGHT": return pb.Direction_DIR_STRAIGHT
    case "LEFT":     return pb.Direction_DIR_LEFT
    case "RIGHT":    return pb.Direction_DIR_RIGHT
    default:         return pb.Direction_DIR_UNKNOWN
    }
}

func main() {
    time.Sleep(2 * time.Second) // Aspetta il Manager

    managerAddr := os.Getenv("MANAGER_ADDR")
    vehicleID := os.Getenv("VEHICLE_ID")
    speed, _ := strconv.ParseFloat(os.Getenv("START_SPEED"), 64)
    distance, _ := strconv.ParseFloat(os.Getenv("START_DISTANCE"), 64)
    lane := parseLane(os.Getenv("ORIGIN_LANE"))
    dir := parseDirection(os.Getenv("DIRECTION"))

    if managerAddr == "" { managerAddr = "localhost:50051" }
    if vehicleID == "" { vehicleID = "CAR_MANUAL" }
    if speed == 0 { speed = 40.0 }
    if distance == 0 { distance = 400.0 }

    // Usa il modulo condiviso (in modo sincrono)
    vehicle.Simulate(managerAddr, vehicleID, speed, distance, lane, dir)
}