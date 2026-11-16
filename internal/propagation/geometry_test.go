package propagation

import (
	"math"
	"testing"
)

func TestParseLocatorValid(t *testing.T) {
	cases := []struct {
		loc      string
		lat, lon float64
	}{
		{"EM38", 38.5, -93.0},
		{"em38", 38.5, -93.0}, // case-insensitive
	}
	for _, c := range cases {
		p, err := ParseLocator(c.loc)
		if err != nil {
			t.Fatalf("ParseLocator(%q): %v", c.loc, err)
		}
		if math.Abs(p.Lat-c.lat) > 1e-6 || math.Abs(p.Lon-c.lon) > 1e-6 {
			t.Errorf("ParseLocator(%q) = (%v, %v), want (%v, %v)", c.loc, p.Lat, p.Lon, c.lat, c.lon)
		}
	}

	// 6-character subsquare center should be within the 4-char square.
	p, err := ParseLocator("EM38xx")
	if err != nil {
		t.Fatalf("ParseLocator(EM38xx): %v", err)
	}
	if math.Abs(p.Lat-38.98) > 0.2 || math.Abs(p.Lon-(-92.04)) > 0.2 {
		t.Errorf("ParseLocator(EM38xx) = (%v, %v)", p.Lat, p.Lon)
	}
}

func TestParseLocatorInvalid(t *testing.T) {
	for _, loc := range []string{
		"", "E", "EM3", "EM38X", "ZZ38", "EM3X", "EM38ZZ", "EM38Q1", "  ",
	} {
		if _, err := ParseLocator(loc); err == nil {
			t.Errorf("ParseLocator(%q) expected error", loc)
		}
	}
}

func TestDistanceBearing(t *testing.T) {
	a := Point{Lat: 0, Lon: 0}
	b := Point{Lat: 0, Lon: 90}
	// Quarter of the equator: R * pi/2 ~= 10007 km.
	if d := DistanceKm(a, b); math.Abs(d-10007) > 60 {
		t.Errorf("DistanceKm = %.1f, want ~10007", d)
	}
	if brg := BearingDeg(a, b); math.Abs(brg-90) > 1 {
		t.Errorf("BearingDeg = %.1f, want ~90", brg)
	}
	if d := DistanceKm(a, a); d > 1e-6 {
		t.Errorf("DistanceKm(a,a) = %v, want 0", d)
	}
}

func TestParseKC2GTime(t *testing.T) {
	for _, s := range []string{
		"2026-11-16T20:15:01",
		"2026-11-16T20:15:01Z",
		"2026-11-16T20:15:01.123Z",
	} {
		if _, err := parseKC2GTime(s); err != nil {
			t.Errorf("parseKC2GTime(%q): %v", s, err)
		}
	}
	if _, err := parseKC2GTime("not-a-time"); err == nil {
		t.Error("parseKC2GTime(not-a-time) expected error")
	}
}

func TestScoresInRange(t *testing.T) {
	c := Conditions{SFI: 120, KIndex: 2}
	for _, mhz := range []float64{3.5, 7, 14, 28} {
		if q := hfScore(mhz, 20, 1500, c); q < 0 || q > 100 {
			t.Errorf("hfScore(%.1f) = %d out of range", mhz, q)
		}
		if q := neutralHFScore(1500, mhz, c); q < 0 || q > 100 {
			t.Errorf("neutralHFScore(%.1f) = %d out of range", mhz, q)
		}
	}
	if q := vhfScore(20, 50); q < 0 || q > 100 {
		t.Errorf("vhfScore = %d out of range", q)
	}
}
