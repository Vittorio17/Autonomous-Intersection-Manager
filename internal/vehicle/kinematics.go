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
	MaxAccel    float64
	MaxDecel    float64
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
		MaxAccel:    maxAccel,
		MaxDecel:    maxDecel,
	}
}

// SetTargetFromETA calcola la TargetSpeed tale che, considerando la rampa di
// accelerazione o decelerazione, il tempo totale d'arrivo coincida con targetETA.
func (k *Kinematics) SetTargetFromETA(targetETA float64) {
	if targetETA <= 0 || k.Distance <= 0 {
		return
	}

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
		// Dobbiamo accelerare (a = +MaxAccel).
		// d = v_f * T - (v_f - v_0)^2 / (2 * accel)
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
	if dt <= 0 || k.Distance <= 0 {
		return
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

	if k.Speed < MinSpeed {
		k.Speed = MinSpeed
	}

	avgSpeed := (prevSpeed + k.Speed) / 2.0
	k.Distance -= avgSpeed * dt
	if k.Distance < 0 {
		k.Distance = 0
	}
}

func (k *Kinematics) EstimateETA() float64 {
	if k.Distance <= 0 {
		return 0
	}

	diff := k.TargetSpeed - k.Speed
	if math.Abs(diff) < 1e-6 {
		return k.Distance / k.Speed
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
	return tRamp + (remainingDist / k.TargetSpeed)
}