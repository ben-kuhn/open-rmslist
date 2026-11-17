package propagation

import "testing"

func TestGroundwaveScore(t *testing.T) {
	cases := []struct {
		name   string
		distKm float64
		mhz    float64
		want   int
	}{
		{"40m very short", 2.5, 7.1, 92},
		{"80m very short", 2.5, 3.9, 95},
		{"20m very short", 2.5, 14.1, 65},
		{"10m very short", 2.5, 28.5, 24},
		{"40m beyond reach", 200, 7.1, 0},
		{"80m beyond reach", 400, 3.9, 0},
	}
	for _, c := range cases {
		if got := GroundwaveScore(c.distKm, c.mhz); got != c.want {
			t.Errorf("%s: GroundwaveScore(%.1f, %.1f) = %d, want %d",
				c.name, c.distKm, c.mhz, got, c.want)
		}
	}
}

func TestGroundwaveScoreTapers(t *testing.T) {
	// 40 m reaches 45 km; the score must stay high inside it and decay to zero.
	inside := GroundwaveScore(10, 7.1)
	if inside < 90 {
		t.Fatalf("40m at 10 km = %d, want >= 90", inside)
	}
	mid := GroundwaveScore(60, 7.1)
	if mid <= 0 || mid >= inside {
		t.Fatalf("40m at 60 km = %d, want between 0 and %d", mid, inside)
	}
	if far := GroundwaveScore(100, 7.1); far != 0 {
		t.Fatalf("40m at 100 km = %d, want 0", far)
	}
}

func TestGroundwaveFloorRaisesShortHF(t *testing.T) {
	// Simulate the reported case: a 40 m gateway 2.5 km away scored only ~57%.
	p := &Prediction{LinkQuality: 57, BandOpen: true, Note: "VOACAP (reliability 95%)"}
	applyGroundwaveFloor(p, 2.5, 7.1)
	if p.LinkQuality < 90 {
		t.Fatalf("LinkQuality = %d, want >= 90", p.LinkQuality)
	}
	if got := GroundwaveScore(2.5, 7.1); p.LinkQuality != got {
		t.Fatalf("LinkQuality = %d, want %d", p.LinkQuality, got)
	}

	// A long path must be untouched.
	long := &Prediction{LinkQuality: 57, Note: "x"}
	applyGroundwaveFloor(long, 1500, 7.1)
	if long.LinkQuality != 57 || long.Note != "x" {
		t.Fatalf("long path changed: %+v", long)
	}
}
