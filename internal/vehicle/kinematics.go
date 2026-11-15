package vehicle

import "math"

const (
	DefaultMaxAccel = 2.5 // m/s²
	DefaultMaxDecel = 3.5 // m/s²
	MinSpeed        = 1.0 // m/s
)

type Kinematics struct {
	Distance    float64
	Speed       float64
	TargetSpeed float64
	CruiseSpeed float64
	MaxAccel    float64
	MaxDecel    float64
	StopDistance *float64
}

func NewKinematics(distance, initialSpeed, maxAccel, maxDecel float64) *Kinematics {
	if maxAccel <= 0 {
		maxAccel = DefaultMaxAccel
	}
	if maxDecel <= 0 {
		maxDecel = DefaultMaxDecel
	}
	return &Kinematics{
		Distance:    distance,
		Speed:       initialSpeed,
		TargetSpeed: initialSpeed,
		CruiseSpeed: initialSpeed,
		MaxAccel:    maxAccel,
		MaxDecel:    maxDecel,
	}
}


// SetStopTarget impone l'arresto completo (speed = 0) a stopDist metri dalla stop-line.
func (k *Kinematics) SetStopTarget(stopDist float64) {
	if stopDist < 0 {
		stopDist = 0
	}
	k.StopDistance = &stopDist
}

// ClearStopTarget rimuove il vincolo di arresto (es. semaforo verde) e ripristina la velocità di crociera.
func (k *Kinematics) ClearStopTarget() {
	if k.StopDistance != nil {
		k.StopDistance = nil
		k.TargetSpeed = k.CruiseSpeed
	}
}

func (k *Kinematics) SetTargetFromETA(targetETA float64) {
	if targetETA <= 0 || k.Distance <= 0 {
		return
	}
	k.StopDistance = nil

	avgNeeded := k.Distance / targetETA
	var desired float64

	if avgNeeded < k.Speed {
		a := k.MaxDecel
		b := a*targetETA - k.Speed
		disc := b*b - (k.Speed*k.Speed - 2*a*k.Distance)
		if disc >= 0 {
			desired = -b + math.Sqrt(disc)
		} else {
			desired = avgNeeded
		}
	} else {
		a := k.MaxAccel
		b := a*targetETA + k.Speed
		disc := b*b - (k.Speed*k.Speed + 2*a*k.Distance)
		if disc >= 0 {
			desired = b - math.Sqrt(disc)
		} else {
			desired = avgNeeded
		}
	}

	if desired < MinSpeed {
		desired = MinSpeed
	}
	k.TargetSpeed = desired
}

func (k *Kinematics) Step(dt float64) {
	if dt <= 0 {
		return
	}

	// Se c'è un target di arresto (rosso o coda), calcola il profilo di frenata fino a speed = 0
	if k.StopDistance != nil && k.Distance > 0 {
		distToStop := math.Max(0, k.Distance-*k.StopDistance)
		if distToStop <= 0.05 {
			k.Speed = 0
			k.Distance = *k.StopDistance
			return
		}
		// v = sqrt(2 * a_decel * d) per fermarsi esattamente a *StopDistance
		safeApproachSpeed := math.Sqrt(2.0 * k.MaxDecel * distToStop)
		k.TargetSpeed = math.Min(k.CruiseSpeed, safeApproachSpeed)
	}

	prevSpeed := k.Speed
	diff := k.TargetSpeed - k.Speed

	if diff > 0 {
		deltaV := math.Min(diff, k.MaxAccel*dt)
		k.Speed += deltaV
	} else if diff < 0 {
		deltaV := math.Max(diff, -k.MaxDecel*dt)
		k.Speed += deltaV
	}

	if k.StopDistance == nil {
		if k.Speed < MinSpeed {
			k.Speed = MinSpeed
		}
	} else if k.Speed < 0 {
		k.Speed = 0
	}

	avgSpeed := (prevSpeed + k.Speed) / 2.0
	k.Distance -= avgSpeed * dt

	// Non oltrepassa mai il punto di arresto quando StopDistance è attivo
	if k.StopDistance != nil && k.Distance < *k.StopDistance {
		k.Distance = *k.StopDistance
		k.Speed = 0
	}
}

func (k *Kinematics) EstimateETA() float64 {
	if k.Distance <= 0 {
		return 0
	}
	if k.Speed <= 0.01 && k.StopDistance != nil {
		return 999.0 // Veicolo fermo al rosso/in coda
	}

	diff := k.TargetSpeed - k.Speed
	if math.Abs(diff) < 1e-6 {
		eff := math.Max(k.Speed, MinSpeed)
		return k.Distance / eff
	}

	var accelRate, signedAccel float64
	if diff > 0 {
		accelRate = k.MaxAccel
		signedAccel = k.MaxAccel
	} else {
		accelRate = k.MaxDecel
		signedAccel = -k.MaxDecel
	}

	tRamp := math.Abs(diff) / accelRate
	dRamp := ((k.Speed + k.TargetSpeed) / 2.0) * tRamp

	if dRamp >= k.Distance {
		discriminant := k.Speed*k.Speed + 2.0*signedAccel*k.Distance
		if discriminant < 0 {
			return k.Distance / math.Max(k.Speed/2.0, MinSpeed)
		}
		return (-k.Speed + math.Sqrt(discriminant)) / signedAccel
	}

	remainingDist := k.Distance - dRamp
	effTarget := math.Max(k.TargetSpeed, MinSpeed)
	return tRamp + (remainingDist / effTarget)
}