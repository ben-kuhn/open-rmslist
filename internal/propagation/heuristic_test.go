package propagation

import (
	"math"
	"testing"
)

func TestPathMUF(t *testing.T) {
	h := NewHeuristic(t.TempDir())
	h.ionosondes = []Ionosonde{
		{Fof2: 5, Mufd: 15, Lat: 40, Lon: -100},
		{Fof2: 6, Mufd: 18, Lat: 41, Lon: -101},
	}
	from := Point{Lat: 40, Lon: -100}
	to := Point{Lat: 41, Lon: -101}
	muf, ok := h.pathMUF(from, to)
	if !ok {
		t.Fatal("expected a MUF estimate")
	}
	if muf < 5 || muf > 18 {
		t.Errorf("pathMUF = %v, want within [5,18]", muf)
	}

	h.ionosondes = nil
	if _, ok := h.pathMUF(from, to); ok {
		t.Error("expected no MUF without ionosondes")
	}
}

func TestConditionsCacheRoundTrip(t *testing.T) {
	h := NewHeuristic(t.TempDir())
	want := Conditions{SFI: 120, AIndex: 5, KIndex: 2, Sunspots: 95}
	iono := []Ionosonde{{Fof2: 5, Mufd: 15, Lat: 40, Lon: -100}}
	h.saveCached(want, iono)

	got, gotIono, err := h.loadCached()
	if err != nil {
		t.Fatal(err)
	}
	if got.SFI != 120 || got.AIndex != 5 || got.KIndex != 2 || got.Sunspots != 95 {
		t.Errorf("conditions round-trip = %+v", got)
	}
	if len(gotIono) != 1 || gotIono[0].Fof2 != 5 || gotIono[0].Mufd != 15 {
		t.Errorf("ionosonde round-trip = %+v", gotIono)
	}
}

func TestMidpointLon(t *testing.T) {
	cases := []struct{ a, b, want float64 }{
		{0, 90, 45},
		{179, -179, 180},
		{-170, 170, -180},
	}
	for _, c := range cases {
		if got := midpointLon(c.a, c.b); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("midpointLon(%v,%v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
