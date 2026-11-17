package propagation

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/ben-kuhn/rms-radar/internal/propagation/voacap"
)

func TestEncodeInput(t *testing.T) {
	var buf bytes.Buffer
	err := voacap.Encode(&buf, voacap.EncodeParams{
		FromLat: 37.5, FromLon: -91.5,
		ToLat: 60.29, ToLon: 5.33,
		FromLabel:          "EM38",
		ToLabel:            "JP20",
		SSN:                121,
		TransmitPower:      50,
		MinSNR:             20,
		Frequency:          14.102,
		MinTakeoffAngle:    3,
		MultipathDelay:     0.1,
		MultipathTolerance: 0.5,
		LocalNoise:         145,
	})
	if err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{
		"COMMENT", "LINEMAX", "COEFFS", "TIME", "MONTH", "SUNSPOT",
		"LABEL", "CIRCUIT", "SYSTEM", "FREQUENCY", "METHOD", "EXECUTE", "QUIT",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing card %q in:\n%s", want, out)
		}
	}
}

// TestParseOutputAndLQI validates the parser and LQI against a real VOACAP
// output fixture (adapted from Pat's MIT-licensed testdata).
func TestParseOutputAndLQI(t *testing.T) {
	f, err := os.Open("testdata/pat_20m.out")
	if err != nil {
		t.Skip("testdata fixture not present")
	}
	defer f.Close()

	out, err := voacap.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Predictions) == 0 {
		t.Fatal("no predictions parsed")
	}
	if out.Request.Frequency == 0 {
		t.Fatal("request frequency not parsed")
	}

	lqi, snr, rel, err := computeLQI(out, out.Request.Frequency)
	if err != nil {
		t.Fatal(err)
	}
	if lqi < 0 || lqi > 100 {
		t.Fatalf("LQI out of range: %d", lqi)
	}
	if snr <= 0 || rel < 0 || rel > 1 {
		t.Fatalf("unexpected SNR/REL: snr=%.1f rel=%.3f", snr, rel)
	}
}
