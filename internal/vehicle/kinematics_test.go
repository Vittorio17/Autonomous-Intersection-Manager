package vehicle

import (
	"math"
	"testing"
)

func TestKinematics_RampRespectsComfortLimits(t *testing.T) {
	kin := NewKinematics(150.0, 25.0, 2.0, 3.0)

	// Chiediamo di arrivare tra 10 secondi (serve rallentare gradualmente)
	kin.SetTargetFromETA(10.0)

	dt := 0.5
	maxDrop := 3.0*dt + 1e-9

	for step := 0; step < 12; step++ {
		prev := kin.Speed
		kin.Step(dt)
		if prev-kin.Speed > maxDrop {
			t.Fatalf("Step %d: decelerazione eccessiva %.4f m/s (limite %.4f m/s)",
				step, prev-kin.Speed, maxDrop)
		}
	}
}

func TestKinematics_ArrivalMatchesTargetETA(t *testing.T) {
	// Veicolo a 120m a 20 m/s (ETA iniziale = 6s).
	// Il Manager rigetta e chiede di arrivare tra 9.0s.
	kin := NewKinematics(120.0, 20.0, 2.5, 3.5)
	targetETA := 9.0
	kin.SetTargetFromETA(targetETA)

	// Verifica che l'ETA ricalcolato subito dopo corrisponda a targetETA
	if gotETA := kin.EstimateETA(); math.Abs(gotETA-targetETA) > 1e-5 {
		t.Errorf("EstimateETA dopo SetTargetFromETA = %.5f; want %.5f", gotETA, targetETA)
	}

	// Simuliamo a passo fine (0.01s) fino all'arrivo
	dt := 0.01
	elapsed := 0.0
	for kin.Distance > 0 {
		kin.Step(dt)
		elapsed += dt
	}

	// Tolleranza di un singolo step discreto
	if math.Abs(elapsed-targetETA) > 0.02 {
		t.Errorf("Arrivo effettivo = %.3fs; concordato = %.3fs (fuori tolleranza)", elapsed, targetETA)
	}
}