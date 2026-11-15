package main

import (
	"testing"

	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
)

func TestTrafficGenerator_DeterministicSeed(t *testing.T) {
	cfg := SpawnerConfig{
		Profile:        "burst",
		Seed:           12345,
		EmergencyRatio: 0.15,
	}
	ApplyProfile(&cfg)

	gen1 := NewTrafficGenerator(cfg)
	gen2 := NewTrafficGenerator(cfg)

	seq1 := gen1.GenerateSequence(30)
	seq2 := gen2.GenerateSequence(30)

	if len(seq1) != 30 || len(seq2) != 30 {
		t.Fatalf("Attesi 30 veicoli, ottenuti %d e %d", len(seq1), len(seq2))
	}

	for i := range seq1 {
		if seq1[i] != seq2[i] {
			t.Fatalf("Divergenza all'indice %d con stesso seed: %+v vs %+v", i, seq1[i], seq2[i])
		}
	}
}

func TestTrafficGenerator_TurnRatioAndBurstLanes(t *testing.T) {
	cfg := SpawnerConfig{
		Profile:   "normal",
		Seed:      99,
		TurnRatio: 0.0, // Nessuna svolta consentita
		BurstSize: 4,   // 4 veicoli simultanei devono occupare 4 corsie distinte
	}

	gen := NewTrafficGenerator(cfg)
	batch := gen.NextBatch()

	if len(batch) != 4 {
		t.Fatalf("Atteso batch da 4 veicoli, ottenuto %d", len(batch))
	}

	seenLanes := make(map[pb.Lane]bool)
	for _, v := range batch {
		if v.Direction != pb.Direction_DIR_STRAIGHT {
			t.Errorf("Attesa DIR_STRAIGHT con TurnRatio=0, ottenuto %v", v.Direction)
		}
		if seenLanes[v.Lane] {
			t.Errorf("Corsia duplicata %v nello stesso burst da 4 veicoli", v.Lane)
		}
		seenLanes[v.Lane] = true
	}
}