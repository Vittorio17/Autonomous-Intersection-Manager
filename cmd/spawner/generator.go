package main

import (
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"sync"
	"time"

	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
)

// SpawnerConfig racchiude i parametri di generazione del traffico.
type SpawnerConfig struct {
	ManagerAddr    string
	Profile        string
	Seed           int64
	Interval       time.Duration
	TurnRatio      float64
	EmergencyRatio float64
	BurstSize      int
	MaxVehicles    int
}

// SpawnedVehicle rappresenta la descrizione deterministica di un singolo veicolo generato.
type SpawnedVehicle struct {
	VehicleID   string
	Speed       float64
	Distance    float64
	Lane        pb.Lane
	Direction   pb.Direction
	IsEmergency bool
	DelayBefore time.Duration
}

// ApplyProfile imposta i parametri predefiniti del profilo scelto.
func ApplyProfile(cfg *SpawnerConfig) {
	switch cfg.Profile {
	case "burst":
		cfg.Interval = 2500 * time.Millisecond
		cfg.BurstSize = 3
		cfg.TurnRatio = 0.50
	case "high_congestion":
		cfg.Interval = 350 * time.Millisecond
		cfg.BurstSize = 2
		cfg.TurnRatio = 0.55
	default:
		cfg.Profile = "normal"
		cfg.Interval = 1500 * time.Millisecond
		cfg.BurstSize = 1
		cfg.TurnRatio = 0.40
	}
}

// TrafficGenerator genera flussi deterministici di veicoli a partire da un seed locale.
type TrafficGenerator struct {
	cfg     SpawnerConfig
	rng     *rand.Rand
	counter int
}

func NewTrafficGenerator(cfg SpawnerConfig) *TrafficGenerator {
	if cfg.BurstSize < 1 {
		cfg.BurstSize = 1
	}
	return &TrafficGenerator{
		cfg: cfg,
		rng: rand.New(rand.NewSource(cfg.Seed)),
	}
}

func (g *TrafficGenerator) nextLane(used map[pb.Lane]bool) pb.Lane {
	lanes := []pb.Lane{pb.Lane_LANE_NORTH, pb.Lane_LANE_SOUTH, pb.Lane_LANE_EAST, pb.Lane_LANE_WEST}
	// Nei burst prova a scegliere corsie distinte per massimizzare i conflitti simultanei sull'incrocio
	if len(used) < len(lanes) {
		var available []pb.Lane
		for _, l := range lanes {
			if !used[l] {
				available = append(available, l)
			}
		}
		chosen := available[g.rng.Intn(len(available))]
		used[chosen] = true
		return chosen
	}
	return lanes[g.rng.Intn(len(lanes))]
}

func (g *TrafficGenerator) nextDirection() pb.Direction {
	if g.rng.Float64() >= g.cfg.TurnRatio {
		return pb.Direction_DIR_STRAIGHT
	}
	if g.rng.Float64() < 0.5 {
		return pb.Direction_DIR_LEFT
	}
	return pb.Direction_DIR_RIGHT
}

// NextBatch produce la prossima ondata (1 o più veicoli simultanei) in modo completamente deterministico.
func (g *TrafficGenerator) NextBatch() []SpawnedVehicle {
	count := g.cfg.BurstSize
	if g.cfg.MaxVehicles > 0 && g.counter+count > g.cfg.MaxVehicles {
		count = g.cfg.MaxVehicles - g.counter
	}
	if count <= 0 {
		return nil
	}

	usedLanes := make(map[pb.Lane]bool, count)
	batch := make([]SpawnedVehicle, 0, count)

	// In modalità burst/high_congestion allineiamo leggermente velocità e distanza per forzare ETA ravvicinati
	baseDist := 250.0 + g.rng.Float64()*150.0
	baseSpeed := 25.0 + g.rng.Float64()*15.0

	for i := 0; i < count; i++ {
		g.counter++
		isEmergency := g.rng.Float64() < g.cfg.EmergencyRatio
		prefix := "CAR"
		if isEmergency {
			prefix = "AMB"
		}

		var speed, dist float64
		if g.cfg.BurstSize > 1 {
			speed = baseSpeed + (g.rng.Float64()*4.0 - 2.0)
			dist = baseDist + (g.rng.Float64()*20.0 - 10.0)
		} else {
			speed = 30.0 + g.rng.Float64()*20.0
			dist = 300.0 + g.rng.Float64()*200.0
		}

		delay := time.Duration(0)
		if i == 0 && g.counter > count {
			delay = g.cfg.Interval
		}

		batch = append(batch, SpawnedVehicle{
			VehicleID:   fmt.Sprintf("%s_%03d", prefix, g.counter),
			Speed:       speed,
			Distance:    dist,
			Lane:        g.nextLane(usedLanes),
			Direction:   g.nextDirection(),
			IsEmergency: isEmergency,
			DelayBefore: delay,
		})
	}

	return batch
}

// GenerateSequence genera l'intera sequenza di N veicoli (utile per test di riproducibilità e benchmark).
func (g *TrafficGenerator) GenerateSequence(n int) []SpawnedVehicle {
	seq := make([]SpawnedVehicle, 0, n)
	for len(seq) < n {
		batch := g.NextBatch()
		if len(batch) == 0 {
			break
		}
		for _, v := range batch {
			if len(seq) < n {
				seq = append(seq, v)
			}
		}
	}
	return seq
}

// Run esegue la generazione del traffico invocando spawnFn per ogni veicolo.
func Run(cfg SpawnerConfig, spawnFn func(SpawnedVehicle)) {
	gen := NewTrafficGenerator(cfg)
	var wg sync.WaitGroup

	for {
		batch := gen.NextBatch()
		if len(batch) == 0 {
			break
		}

		for _, v := range batch {
			if v.DelayBefore > 0 {
				time.Sleep(v.DelayBefore)
			}
			vCopy := v
			wg.Add(1)
			go func() {
				defer wg.Done()
				spawnFn(vCopy)
			}()
		}
	}

	wg.Wait()
}

// Helper per leggere fallback da variabili d'ambiente
func envOrDefault(key, def string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return def
}

func envIntOrDefault(key string, def int) int {
	if val := os.Getenv(key); val != "" {
		if parsed, err := strconv.Atoi(val); err == nil {
			return parsed
		}
	}
	return def
}

func envInt64OrDefault(key string, def int64) int64 {
	if val := os.Getenv(key); val != "" {
		if parsed, err := strconv.ParseInt(val, 10, 64); err == nil {
			return parsed
		}
	}
	return def
}

func envFloatOrDefault(key string, def float64) float64 {
	if val := os.Getenv(key); val != "" {
		if parsed, err := strconv.ParseFloat(val, 64); err == nil {
			return parsed
		}
	}
	return def
}

func envDurationOrDefault(key string, def time.Duration) time.Duration {
	if val := os.Getenv(key); val != "" {
		if parsed, err := time.ParseDuration(val); err == nil {
			return parsed
		}
	}
	return def
}