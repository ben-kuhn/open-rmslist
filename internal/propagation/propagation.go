package propagation

import (
	"context"
	"errors"
)

// Prediction is the result of estimating link success for a path.
type Prediction struct {
	LinkQuality  int     `json:"link_quality"` // 0-100
	BandOpen     bool    `json:"band_open"`
	MUF          float64 `json:"muf_mhz,omitempty"`
	FrequencyMHz float64 `json:"frequency_mhz"`
	DistanceKm   float64 `json:"distance_km"`
	BearingDeg   float64 `json:"bearing_deg"`
	SNRdB        float64 `json:"snr_db,omitempty"`
	Method       string  `json:"method"` // "voacap" or "heuristic"
	Note         string  `json:"note,omitempty"`
}

// Params describes a single point-to-point link estimate.
type Params struct {
	FromLat, FromLon float64
	ToLat, ToLon     float64
	FrequencyHz      float64
	Mode             string
	RadioRangeKm     float64
	SSN              int // smoothed sunspot number, used by the VOACAP predictor
}

// Predictor estimates link success for a path.
type Predictor interface {
	Available() bool
	Predict(ctx context.Context, p Params) (*Prediction, error)
}

// ErrUnavailable is returned when no predictor is available.
var ErrUnavailable = errors.New("predictor unavailable")

// Ranker tries predictors in order, falling back until one succeeds. Order the
// predictors so that the most accurate available one comes first (e.g. VOACAP,
// then the always-available heuristic).
type Ranker struct {
	Predictors []Predictor
}

func (r *Ranker) Predict(ctx context.Context, p Params) (*Prediction, error) {
	var lastErr error
	for _, pr := range r.Predictors {
		if pr == nil || !pr.Available() {
			continue
		}
		pred, err := pr.Predict(ctx, p)
		if err == nil {
			return pred, nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, ErrUnavailable
}
