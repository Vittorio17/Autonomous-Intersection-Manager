package main

import (
	"testing"
	"time"

	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
)

func TestTrafficLight_PhaseCycle(t *testing.T) {
	t0 := time.Unix(1000, 0)
	tl := NewTrafficLightController(t0)

	// t=0s: Verde Nord-Sud, Rosso Est-Ovest
	if got := tl.CurrentLight(pb.Lane_LANE_NORTH, t0); got != LightGreen {
		t.Errorf("t=0s Nord atteso GREEN, ottenuto %v", got)
	}
	if got := tl.CurrentLight(pb.Lane_LANE_EAST, t0); got != LightRed {
		t.Errorf("t=0s Est atteso RED, ottenuto %v", got)
	}

	// t=12s: Giallo Nord-Sud, Rosso Est-Ovest
	t12 := t0.Add(12 * time.Second)
	if got := tl.CurrentLight(pb.Lane_LANE_SOUTH, t12); got != LightYellow {
		t.Errorf("t=12s Sud atteso YELLOW, ottenuto %v", got)
	}
	if got := tl.CurrentLight(pb.Lane_LANE_WEST, t12); got != LightRed {
		t.Errorf("t=12s Ovest atteso RED, ottenuto %v", got)
	}

	// t=15s: Rosso Nord-Sud, Verde Est-Ovest
	t15 := t0.Add(15 * time.Second)
	if got := tl.CurrentLight(pb.Lane_LANE_NORTH, t15); got != LightRed {
		t.Errorf("t=15s Nord atteso RED, ottenuto %v", got)
	}
	if got := tl.CurrentLight(pb.Lane_LANE_EAST, t15); got != LightGreen {
		t.Errorf("t=15s Est atteso GREEN, ottenuto %v", got)
	}

	// t=27s: Giallo Est-Ovest
	t27 := t0.Add(27 * time.Second)
	if got := tl.CurrentLight(pb.Lane_LANE_WEST, t27); got != LightYellow {
		t.Errorf("t=27s Ovest atteso YELLOW, ottenuto %v", got)
	}

	// t=30s: Ciclo completo ricomincia da Verde Nord-Sud
	t30 := t0.Add(30 * time.Second)
	if got := tl.CurrentLight(pb.Lane_LANE_NORTH, t30); got != LightGreen {
		t.Errorf("t=30s Nord atteso GREEN, ottenuto %v", got)
	}
}

func TestTrafficLight_RedLightAndQueueStacking(t *testing.T) {
	t0 := time.Unix(1000, 0)
	tl := NewTrafficLightController(t0)

	// Est ha semaforo ROSSO a t=0
	req1 := &pb.VehicleRequest{
		VehicleId:      "EAST_1",
		OriginLane:     pb.Lane_LANE_EAST,
		Direction:      pb.Direction_DIR_STRAIGHT,
		DistanceM:      20.0,
		Speed:          15.0,
		Eta:            1.33,
		VehicleLengthM: 4.5,
	}
	st1, _, stop1 := tl.Evaluate(req1, t0)
	if st1 != pb.CommandStatus_STATUS_REJECT || stop1 != 0.0 {
		t.Fatalf("Primo veicolo al rosso doveva fermarsi a stop=0.0m, ottenuto status=%v stop=%.2f", st1, stop1)
	}

	// Secondo veicolo sulla stessa corsia Est dietro a EAST_1
	req2 := &pb.VehicleRequest{
		VehicleId:      "EAST_2",
		OriginLane:     pb.Lane_LANE_EAST,
		Direction:      pb.Direction_DIR_STRAIGHT,
		DistanceM:      50.0,
		Speed:          15.0,
		Eta:            3.33,
		VehicleLengthM: 4.5,
	}
	st2, _, stop2 := tl.Evaluate(req2, t0)
	expectedStop2 := 20.0 + 4.5 + minQueueGapM // 27.5m
	if st2 != pb.CommandStatus_STATUS_REJECT || stop2 != expectedStop2 {
		t.Fatalf("Secondo veicolo doveva accodarsi a stop=%.2fm, ottenuto status=%v stop=%.2f", expectedStop2, st2, stop2)
	}
}