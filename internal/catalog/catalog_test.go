package catalog

import (
	"testing"

	"github.com/ben-kuhn/open-rmslist/internal/cmsapi"
)

func TestModeFamily(t *testing.T) {
	cases := map[string]string{
		"VARA": "VARA", "VARA FM": "VARA FM", "VARA FM WIDE": "VARA FM",
		"Mercury": "Mercury", "VardeFM": "VardeFM",
		"ARDOP 2000":  "ARDOP",
		"Packet 1200": "Packet", "Pactor 3": "PACTOR", "Robust Packet": "Robust Packet",
		"WINMOR 1600": "WINMOR", "Unknown": "Other",
	}
	for mode, want := range cases {
		if got := modeFamily(mode); got != want {
			t.Errorf("modeFamily(%q) = %q, want %q", mode, got, want)
		}
	}
}

func TestBandForFrequency(t *testing.T) {
	cases := map[float64]string{
		7.103e6:  "40m",
		14.1e6:   "20m",
		3.592e6:  "80m",
		10.147e6: "30m",
		145.03e6: "2m",
		440e6:    "70cm",
		50.1e6:   "6m",
		28.5e6:   "10m",
		1.5e6:    "HF",
		1000e6:   "Other",
	}
	for hz, want := range cases {
		if got := bandForFrequency(hz); got != want {
			t.Errorf("bandForFrequency(%v) = %q, want %q", hz, got, want)
		}
	}
}

func TestFormatFrequency(t *testing.T) {
	cases := map[float64]string{
		0:         "",
		7103000:   "7.103 MHz",
		145030000: "145.03 MHz",
		1500:      "1.5 kHz",
	}
	for hz, want := range cases {
		if got := formatFrequency(hz); got != want {
			t.Errorf("formatFrequency(%v) = %q, want %q", hz, got, want)
		}
	}
}

func TestNormalize(t *testing.T) {
	status := &cmsapi.GatewayStatus{Gateways: []cmsapi.Gateway{{
		Callsign:         "K0TEST-10",
		Latitude:         40,
		Longitude:        -100,
		HoursSinceStatus: 1,
		Channels: []cmsapi.GatewayChannel{{
			SupportedModes: "VARA",
			Frequency:      7103000,
			ServiceCode:    "mercury",
			Gridsquare:     "EM38",
		}},
	}}}

	stations := Normalize(status)
	if len(stations) != 1 {
		t.Fatalf("got %d stations", len(stations))
	}
	ch := stations[0].Channels[0]
	if ch.ServiceCode != "MERCURY" {
		t.Errorf("ServiceCode = %q, want MERCURY (uppercased)", ch.ServiceCode)
	}
	if ch.Mode != "Mercury" {
		t.Errorf("Mode = %q, want Mercury (service-code label)", ch.Mode)
	}
	if ch.RawMode != "VARA" {
		t.Errorf("RawMode = %q, want VARA (as reported)", ch.RawMode)
	}
	if ch.Band != "40m" {
		t.Errorf("Band = %q, want 40m", ch.Band)
	}
}

func TestCommunityModemLabels(t *testing.T) {
	// Community modem codes are labelled with the service name.
	for sc, want := range map[string]string{"MERCURY": "Mercury", "VARDEFM": "VardeFM"} {
		ch := normalizeChannel(cmsapi.GatewayChannel{SupportedModes: "VARA", ServiceCode: sc})
		if ch.Mode != want {
			t.Errorf("mode for %s = %q, want %q", sc, ch.Mode, want)
		}
		if ch.RawMode != "VARA" {
			t.Errorf("raw_mode for %s = %q, want VARA", sc, ch.RawMode)
		}
	}

	// Access codes keep the mode the API reports.
	emcomm := normalizeChannel(cmsapi.GatewayChannel{SupportedModes: "ARDOP 2000", ServiceCode: "EMCOMM"})
	if emcomm.Mode != "ARDOP 2000" || emcomm.RawMode != "ARDOP 2000" {
		t.Errorf("EMCOMM mode = %q / raw %q, want ARDOP 2000", emcomm.Mode, emcomm.RawMode)
	}
}
