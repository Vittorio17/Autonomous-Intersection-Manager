package main

import (
	"flag"
	"log/slog"
	"time"

	"github.com/Vittorio17/Autonomous-Intersection-Manager/internal/logger"
	"github.com/Vittorio17/Autonomous-Intersection-Manager/internal/vehicle"
)

func main() {
	logger.Init("traffic-spawner")

	cfg := SpawnerConfig{
		ManagerAddr:    envOrDefault("MANAGER_ADDR", "localhost:50051"),
		Profile:        envOrDefault("SPAWNER_PROFILE", "normal"),
		Seed:           envInt64OrDefault("SPAWNER_SEED", 42),
		EmergencyRatio: envFloatOrDefault("SPAWNER_EMERGENCY_RATIO", 0.10),
		MaxVehicles:    envIntOrDefault("SPAWNER_MAX_VEHICLES", 0),
	}

	flag.StringVar(&cfg.ManagerAddr, "manager", cfg.ManagerAddr, "Indirizzo gRPC del Manager")
	flag.StringVar(&cfg.Profile, "profile", cfg.Profile, "Profilo di traffico: normal, burst, high_congestion")
	flag.Int64Var(&cfg.Seed, "seed", cfg.Seed, "Seed deterministico per riproducibilità")

	var intervalOverride time.Duration
	var turnRatioOverride float64
	var burstSizeOverride int

	flag.IntVar(&cfg.MaxVehicles, "max-vehicles", cfg.MaxVehicles, "Numero massimo di veicoli da generare (0 = infinito)")
	flag.DurationVar(&intervalOverride, "interval", 0, "Intervallo tra arrivi/ondate (es. 500ms, 1.5s)")
	flag.Float64Var(&turnRatioOverride, "turn-ratio", -1, "Probabilità di svolta tra 0.0 e 1.0")
	flag.IntVar(&burstSizeOverride, "burst-size", 0, "Numero di veicoli simultanei per ondata")

	// Applica i default del profilo prima di parsare eventuali override granulari
	flag.Parse()
	ApplyProfile(&cfg)

	if intervalOverride > 0 {
		cfg.Interval = intervalOverride
	}
	if turnRatioOverride >= 0 && turnRatioOverride <= 1 {
		cfg.TurnRatio = turnRatioOverride
	}
	if burstSizeOverride > 0 {
		cfg.BurstSize = burstSizeOverride
	}
	
	// Override opzionali dopo ApplyProfile
	if v := envDurationOrDefault("SPAWNER_INTERVAL", 0); v > 0 {
		cfg.Interval = v
	}
	if v := envFloatOrDefault("SPAWNER_TURN_RATIO", -1); v >= 0 && v <= 1 {
		cfg.TurnRatio = v
	}
	if v := envIntOrDefault("SPAWNER_BURST_SIZE", 0); v > 0 {
		cfg.BurstSize = v
	}

	slog.Info("Traffic Spawner avviato",
		slog.String("manager_addr", cfg.ManagerAddr),
		slog.String("profile", cfg.Profile),
		slog.Int64("seed", cfg.Seed),
		slog.Duration("interval", cfg.Interval),
		slog.Int("burst_size", cfg.BurstSize),
		slog.Float64("turn_ratio", cfg.TurnRatio),
		slog.Int("max_vehicles", cfg.MaxVehicles),
	)

	time.Sleep(1500 * time.Millisecond) // Attesa breve per avvio Manager

	Run(cfg, func(v SpawnedVehicle) {
		vehicle.Simulate(
			cfg.ManagerAddr,
			v.VehicleID,
			v.Speed,
			v.Distance,
			v.Lane,
			v.Direction,
			v.IsEmergency,
		)
	})

	slog.Info("Traffic Spawner completato")
}