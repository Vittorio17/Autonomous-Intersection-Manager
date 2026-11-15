package main

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/Vittorio17/Autonomous-Intersection-Manager/proto"
)

func TestMetrics_DelayStopsAndEmergencySeparation(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "test_results.json")
	m := NewMetricsWithConfig("traffic_light", outPath)
	t0 := time.Unix(1000, 0)

	// 1. Veicolo ordinario CAR_01:
	// Distanza iniziale 100m + 14m + 4.5m = 118.5m, velocità 20 m/s -> t_ideal = 5.925s
	m.RecordTelemetry(&pb.VehicleRequest{
		VehicleId:      "CAR_01",
		Speed:          20.0,
		DistanceM:      100.0,
		VehicleLengthM: 4.5,
		IsEmergency:    false,
	}, t0)

	// Si ferma al semaforo rosso (Speed = 0.0 < 0.5 m/s) -> StopCount = 1
	m.RecordTelemetry(&pb.VehicleRequest{
		VehicleId:      "CAR_01",
		Speed:          0.0,
		DistanceM:      0.0,
		VehicleLengthM: 4.5,
		IsEmergency:    false,
	}, t0.Add(5*time.Second))

	// Riparte al verde e completa a t0 + 10.925s -> t_actual = 10.925s -> delay = 5.0s
	m.RecordTelemetry(&pb.VehicleRequest{
		VehicleId:      "CAR_01",
		Speed:          20.0,
		DistanceM:      -18.5,
		VehicleLengthM: 4.5,
		IsEmergency:    false,
		HasExited:      true,
	}, t0.Add(10925*time.Millisecond))
	m.RecordVehicleCompletedAt("CAR_01", t0.Add(10925*time.Millisecond))

	// 2. Veicolo di emergenza AMB_01:
	// Distanza iniziale 100m + 14m + 6.5m = 120.5m, velocità 40 m/s -> t_ideal = 3.0125s, nessuna fermata
	m.RecordTelemetry(&pb.VehicleRequest{
		VehicleId:      "AMB_01",
		Speed:          40.0,
		DistanceM:      100.0,
		VehicleLengthM: 6.5,
		IsEmergency:    true,
	}, t0.Add(1*time.Second))

	// Attraversa in 3.0125s esatti -> delay = 0.0s, StopCount = 0
	m.RecordTelemetry(&pb.VehicleRequest{
		VehicleId:      "AMB_01",
		Speed:          40.0,
		DistanceM:      -20.5,
		VehicleLengthM: 6.5,
		IsEmergency:    true,
		HasExited:      true,
	}, t0.Add(4012500*time.Microsecond))
	m.RecordVehicleCompletedAt("AMB_01", t0.Add(4012500*time.Microsecond))

	rep := m.BuildReport()

	if rep.VehiclesCompleted != 2 {
		t.Fatalf("Attesi 2 veicoli completati, ottenuti %d", rep.VehiclesCompleted)
	}
	if rep.TotalStops != 1 {
		t.Errorf("Atteso TotalStops=1, ottenuto %d", rep.TotalStops)
	}
	if math.Abs(rep.OrdinaryAvgDelaySec-5.0) > 0.01 {
		t.Errorf("Atteso OrdinaryAvgDelaySec=5.0s, ottenuto %.3f", rep.OrdinaryAvgDelaySec)
	}
	if rep.EmergencyVehiclesCompleted != 1 {
		t.Errorf("Atteso EmergencyVehiclesCompleted=1, ottenuto %d", rep.EmergencyVehiclesCompleted)
	}
	if math.Abs(rep.EmergencyAvgTimeSec-3.013) > 0.01 {
		t.Errorf("Atteso EmergencyAvgTimeSec~3.013s, ottenuto %.3f", rep.EmergencyAvgTimeSec)
	}
	if rep.EmergencyAvgDelaySec != 0 {
		t.Errorf("Atteso EmergencyAvgDelaySec=0, ottenuto %.3f", rep.EmergencyAvgDelaySec)
	}
	if rep.EnergyPenaltyIndex <= 0 {
		t.Errorf("Atteso EnergyPenaltyIndex > 0 per la frenata/ripartenza di CAR_01, ottenuto %.3f", rep.EnergyPenaltyIndex)
	}

	// Verifica esportazione JSON
	if err := m.ExportJSON(outPath); err != nil {
		t.Fatalf("ExportJSON fallito: %v", err)
	}
	raw, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("Lettura file JSON fallita: %v", err)
	}
	var parsed SimulationReport
	if err := json.Unmarshal(raw, &parsed); err != nil {
		t.Fatalf("JSON non valido: %v", err)
	}
	if parsed.Mode != "traffic_light" || parsed.TotalStops != 1 {
		t.Errorf("Contenuto JSON inatteso: %+v", parsed)
	}
}