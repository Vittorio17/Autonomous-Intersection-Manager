package main

import (
	"fmt"
	"math/rand"
	"testing"

	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
)

func TestReservationGrid_LeftTurnLongerThanStraight(t *testing.T) {
	speed := 15.0
	lenM := 4.5

	straightCross, straightBuf := ComputeCrossingDurationMs(pb.Direction_DIR_STRAIGHT, lenM, speed)
	leftCross, leftBuf := ComputeCrossingDurationMs(pb.Direction_DIR_LEFT, lenM, speed)

	if leftCross <= straightCross {
		t.Errorf("Attesa durata attraversamento DIR_LEFT (%dms) > DIR_STRAIGHT (%dms)", leftCross, straightCross)
	}
	if leftBuf != straightBuf {
		t.Errorf("Atteso stesso buffer dinamico a parità di velocità/lunghezza: %d vs %d", leftBuf, straightBuf)
	}
}

func TestReservationGrid_SeparatesConflictingIntervals(t *testing.T) {
	grid := NewReservationGrid()
	now := int64(1_000_000)

	// Veicolo 1: Nord -> Dritto (impegna NW, SW) con ETA 5.0s
	req1 := &pb.VehicleRequest{
		VehicleId:      "CAR_1",
		Speed:          20.0,
		Eta:            5.0,
		OriginLane:     pb.Lane_LANE_NORTH,
		Direction:      pb.Direction_DIR_STRAIGHT,
		VehicleLengthM: 4.5,
	}
	st1, _, entry1, exit1 := grid.ReserveOrSuggest(req1, now, nil, nil)
	if st1 != pb.CommandStatus_STATUS_ACK_LOCK {
		t.Fatalf("Atteso ACK_LOCK per CAR_1, ottenuto %v", st1)
	}

	// Veicolo 2: Est -> Dritto (impegna NE, NW -> conflitto su NW!) con stesso ETA 5.0s
	req2 := &pb.VehicleRequest{
		VehicleId:      "CAR_2",
		Speed:          20.0,
		Eta:            5.0,
		OriginLane:     pb.Lane_LANE_EAST,
		Direction:      pb.Direction_DIR_STRAIGHT,
		VehicleLengthM: 4.5,
	}
	st2, _, entry2, exit2 := grid.ReserveOrSuggest(req2, now, nil, nil)
	if st2 != pb.CommandStatus_STATUS_REJECT {
		t.Fatalf("Atteso REJECT per conflitto su NW, ottenuto %v", st2)
	}
	if entry2 <= exit1 {
		t.Errorf("CAR_2 entry (%d) deve essere strettamente successivo a CAR_1 exit (%d)", entry2, exit1)
	}
	if exit2 <= entry2 {
		t.Errorf("Intervallo invalido per CAR_2: [%d, %d]", entry2, exit2)
	}
	_ = entry1
}

func TestReservationGrid_Concurrent60VehiclesNoOverlap(t *testing.T) {
	grid := NewReservationGrid()
	rng := rand.New(rand.NewSource(42))
	now := int64(5_000_000)

	lanes := []pb.Lane{pb.Lane_LANE_NORTH, pb.Lane_LANE_SOUTH, pb.Lane_LANE_EAST, pb.Lane_LANE_WEST}
	dirs := []pb.Direction{pb.Direction_DIR_STRAIGHT, pb.Direction_DIR_LEFT, pb.Direction_DIR_RIGHT}

	// Simuliamo 60 veicoli che negoziano fino ad ottenere STATUS_ACK_LOCK
	for i := 0; i < 60; i++ {
		req := &pb.VehicleRequest{
			VehicleId:      fmt.Sprintf("SIM_%03d", i),
			Speed:          15.0 + rng.Float64()*25.0,
			Eta:            2.0 + rng.Float64()*10.0,
			OriginLane:     lanes[rng.Intn(len(lanes))],
			Direction:      dirs[rng.Intn(len(dirs))],
			VehicleLengthM: 4.5,
		}

		for attempts := 0; attempts < 100; attempts++ {
			st, suggestedETA, _, _ := grid.ReserveOrSuggest(req, now, nil, nil)
			if st == pb.CommandStatus_STATUS_ACK_LOCK {
				break
			}
			req.Eta = suggestedETA
		}
	}

	if grid.Count() != 60 {
		t.Fatalf("Attese 60 prenotazioni confermate nella grid, trovate %d", grid.Count())
	}

	// Verifica invariante: nessuna coppia di prenotazioni con quadranti in comune può sovrapporsi
	grid.mu.Lock()
	defer grid.mu.Unlock()

	var list []GridReservation
	for _, r := range grid.reservations {
		list = append(list, r)
	}

	for i := 0; i < len(list); i++ {
		for j := i + 1; j < len(list); j++ {
			a, b := list[i], list[j]
			if (a.Quadrants & b.Quadrants) != 0 {
				if intervalsOverlap(a.EntryTimeMs, a.EffectiveEndMs, b.EntryTimeMs, b.EffectiveEndMs) {
					t.Fatalf("SOVRAPPOSIZIONE RILEVATA tra %s [%d, %d] e %s [%d, %d] su quadranti %04b",
						a.VehicleID, a.EntryTimeMs, a.EffectiveEndMs,
						b.VehicleID, b.EntryTimeMs, b.EffectiveEndMs,
						a.Quadrants&b.Quadrants,
					)
				}
			}
		}
	}
}