package voacap

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestParseFixture(t *testing.T) {
	f, err := os.Open("../testdata/pat_20m.out")
	if err != nil {
		t.Skip("fixture not present")
	}
	defer f.Close()

	out, err := Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Predictions) == 0 {
		t.Fatal("no predictions parsed")
	}
	if out.Request.Frequency != 14.102 {
		t.Errorf("Request.Frequency = %v", out.Request.Frequency)
	}
	if out.Request.Hour != 8 {
		t.Errorf("Request.Hour = %d, want 8", out.Request.Hour)
	}
}

func TestEncodeDeck(t *testing.T) {
	var buf bytes.Buffer
	err := Encode(&buf, EncodeParams{
		FromLat: 40, FromLon: -100, ToLat: 60, ToLon: 5,
		FromLabel: "A", ToLabel: "B", SSN: 100, TransmitPower: 50, MinSNR: 20,
		Frequency: 14.1, MinTakeoffAngle: 3, MultipathDelay: 0.1,
		MultipathTolerance: 0.5, LocalNoise: 145,
	})
	if err != nil {
		t.Fatal(err)
	}
	got := buf.String()
	for _, want := range []string{"FREQUENCY 14.100", "SUNSPOT", "EXECUTE", "QUIT"} {
		if !strings.Contains(got, want) {
			t.Errorf("deck missing %q:\n%s", want, got)
		}
	}
}
