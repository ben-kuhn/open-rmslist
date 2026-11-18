package propagation

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestHeuristicPredictVHF(t *testing.T) {
	h := NewHeuristic(t.TempDir())
	pred, err := h.Predict(context.Background(), Params{
		FromLat: 40, FromLon: -100, ToLat: 40.2, ToLon: -100,
		FrequencyHz: 145e6, Mode: "VARA FM", RadioRangeKm: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pred.Method != "heuristic" || !pred.BandOpen || pred.LinkQuality < 0 || pred.LinkQuality > 100 {
		t.Fatalf("unexpected VHF prediction: %+v", pred)
	}
}

func TestHeuristicPredictHF(t *testing.T) {
	h := NewHeuristic(t.TempDir())
	now := time.Now()
	h.cond = Conditions{Updated: now, SFI: 150, AIndex: 5, KIndex: 2, Sunspots: 100}
	h.ionosondes = []Ionosonde{{Fof2: 7, Mufd: 14, Lat: 40.5, Lon: -100, Time: now}}
	pred, err := h.Predict(context.Background(), Params{
		FromLat: 40, FromLon: -100, ToLat: 41, ToLon: -100,
		FrequencyHz: 14.1e6, Mode: "VARA", SSN: 100,
	})
	if err != nil {
		t.Fatal(err)
	}
	if pred.Method != "heuristic" || pred.MUF <= 0 {
		t.Fatalf("unexpected HF prediction: %+v", pred)
	}
	if pred.LinkQuality < 0 || pred.LinkQuality > 100 {
		t.Fatalf("LQI out of range: %d", pred.LinkQuality)
	}
}

type fakePredictor struct {
	available bool
	pred      *Prediction
	err       error
}

func (f fakePredictor) Available() bool { return f.available }
func (f fakePredictor) Predict(context.Context, Params) (*Prediction, error) {
	return f.pred, f.err
}

func TestRankerPredict(t *testing.T) {
	want := &Prediction{Method: "fake", LinkQuality: 42}
	r := &Ranker{Predictors: []Predictor{
		fakePredictor{available: false},
		fakePredictor{available: true, err: errors.New("boom")},
		fakePredictor{available: true, pred: want},
	}}
	got, err := r.Predict(context.Background(), Params{})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got %+v want %+v", got, want)
	}

	if _, err := (&Ranker{Predictors: []Predictor{fakePredictor{available: false}}}).Predict(context.Background(), Params{}); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("all unavailable = %v, want ErrUnavailable", err)
	}
}
