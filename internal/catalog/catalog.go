package catalog

import (
	"fmt"
	"sort"
	"strings"

	"github.com/ben-kuhn/open-rmslist/internal/cmsapi"
)

// ModeSwap renames a leading mode token for channels whose service code
// matches. Community modems (Mercury, VardeFM, ...) speak VARA's modem API,
// so the Winlink feed advertises them as "VARA"/"VARA FM" while their actual
// identity is carried in the service code.
type ModeSwap struct {
	ServiceCode string `json:"service_code"`
	From        string `json:"from"` // leading mode token(s) to match
	To          string `json:"to"`   // replacement token(s)
}

// DefaultModeSwaps are the known community modem relabelings.
var DefaultModeSwaps = []ModeSwap{
	// MERCURY gateways are Mercury HF modems advertised as "VARA".
	{ServiceCode: "MERCURY", From: "VARA", To: "Mercury"},
	// VardeFM gateways are VARA FM-compatible modems advertised as "VARA FM".
	{ServiceCode: "VardeFM", From: "VARA FM", To: "VardeFM"},
}

// Channel is a normalized, display-ready channel.
type Channel struct {
	ServiceCode    string  `json:"service_code"`
	Mode           string  `json:"mode"`        // relabeled mode (e.g. "Mercury 500")
	RawMode        string  `json:"raw_mode"`    // as reported by Winlink
	ModeFamily     string  `json:"mode_family"` // VARA, VARA FM, ARDOP, Packet, ...
	FrequencyHz    float64 `json:"frequency_hz"`
	Frequency      string  `json:"frequency"` // human readable
	Gridsquare     string  `json:"gridsquare"`
	OperatingHours string  `json:"operating_hours"`
	Baud           string  `json:"baud,omitempty"`
	RadioRange     string  `json:"radio_range,omitempty"`
	Antenna        string  `json:"antenna,omitempty"`
	Band           string  `json:"band"`
}

// Station is a gateway with its channels flattened and enriched.
type Station struct {
	Callsign         string    `json:"callsign"`
	BaseCallsign     string    `json:"base_callsign"`
	Latitude         float64   `json:"latitude"`
	Longitude        float64   `json:"longitude"`
	HoursSinceStatus int       `json:"hours_since_status"`
	LastStatus       string    `json:"last_status"`
	Comments         string    `json:"comments"`
	Channels         []Channel `json:"channels"`
}

// Normalize converts the raw feed into a flat list of stations, sorted by
// callsign, with modes relabeled according to the swap rules.
func Normalize(status *cmsapi.GatewayStatus, swaps []ModeSwap) []Station {
	if status == nil {
		return nil
	}
	stations := make([]Station, 0, len(status.Gateways))
	for _, gw := range status.Gateways {
		st := Station{
			Callsign:         gw.Callsign,
			BaseCallsign:     gw.BaseCallsign,
			Latitude:         gw.Latitude,
			Longitude:        gw.Longitude,
			HoursSinceStatus: gw.HoursSinceStatus,
			LastStatus:       gw.LastStatus,
			Comments:         gw.Comments,
			Channels:         make([]Channel, 0, len(gw.Channels)),
		}
		for _, ch := range gw.Channels {
			st.Channels = append(st.Channels, normalizeChannel(ch, swaps))
		}
		stations = append(stations, st)
	}
	sort.Slice(stations, func(i, j int) bool {
		return stations[i].Callsign < stations[j].Callsign
	})
	return stations
}

func normalizeChannel(ch cmsapi.GatewayChannel, swaps []ModeSwap) Channel {
	raw := strings.TrimSpace(ch.SupportedModes)
	mode := raw
	if mode == "" {
		mode = "Unknown"
	}
	mode = relabel(mode, ch.ServiceCode, swaps)
	return Channel{
		ServiceCode:    strings.ToUpper(strings.TrimSpace(ch.ServiceCode)),
		Mode:           mode,
		RawMode:        raw,
		ModeFamily:     modeFamily(mode),
		FrequencyHz:    ch.Frequency,
		Frequency:      formatFrequency(ch.Frequency),
		Gridsquare:     ch.Gridsquare,
		OperatingHours: ch.OperatingHours,
		Baud:           ch.Baud,
		RadioRange:     ch.RadioRange,
		Antenna:        ch.Antenna,
		Band:           bandForFrequency(ch.Frequency),
	}
}

// relabel applies the swap whose ServiceCode matches the channel's service
// code. It renames the leading mode token(s) and preserves the remainder.
// A single-token "VARA" swap deliberately leaves "VARA FM"/"VARA FM WIDE"
// untouched (those are FM, not HF).
func relabel(mode, serviceCode string, swaps []ModeSwap) string {
	sc := strings.ToUpper(serviceCode)
	for _, s := range swaps {
		if !strings.EqualFold(s.ServiceCode, sc) {
			continue
		}
		fields := strings.Fields(mode)
		if len(fields) == 0 {
			continue
		}
		fromFields := strings.Fields(s.From)
		if len(fromFields) == 0 || len(fromFields) > len(fields) {
			continue
		}
		// A "VARA" -> X swap should not relabel VARA FM.
		if len(fromFields) == 1 && strings.EqualFold(fromFields[0], "VARA") &&
			len(fields) > 1 && strings.EqualFold(fields[1], "FM") {
			continue
		}
		match := true
		for i := range fromFields {
			if !strings.EqualFold(fields[i], fromFields[i]) {
				match = false
				break
			}
		}
		if !match {
			continue
		}
		out := append([]string{s.To}, fields[len(fromFields):]...)
		return strings.Join(out, " ")
	}
	return mode
}

func modeFamily(mode string) string {
	m := strings.ToUpper(mode)
	switch {
	case strings.HasPrefix(m, "VARA FM"):
		return "VARA FM"
	case strings.HasPrefix(m, "VARA"):
		return "VARA"
	case strings.HasPrefix(m, "MERCURY"):
		return "Mercury"
	case strings.HasPrefix(m, "VARDEFM"):
		return "VardeFM"
	case strings.HasPrefix(m, "ARDOP"):
		return "ARDOP"
	case strings.HasPrefix(m, "PACKET"):
		return "Packet"
	case strings.HasPrefix(m, "PACTOR"):
		return "PACTOR"
	case strings.HasPrefix(m, "ROBUST"):
		return "Robust Packet"
	case strings.HasPrefix(m, "WINMOR"):
		return "WINMOR"
	default:
		return "Other"
	}
}

func formatFrequency(hz float64) string {
	if hz <= 0 {
		return ""
	}
	switch {
	case hz >= 1e9:
		return trimZeros(fmt.Sprintf("%.3f", hz/1e9)) + " GHz"
	case hz >= 1e6:
		return trimZeros(fmt.Sprintf("%.3f", hz/1e6)) + " MHz"
	case hz >= 1e3:
		return trimZeros(fmt.Sprintf("%.3f", hz/1e3)) + " kHz"
	default:
		return fmt.Sprintf("%.0f Hz", hz)
	}
}

func trimZeros(s string) string {
	s = strings.TrimRight(s, "0")
	return strings.TrimRight(s, ".")
}

// bandForFrequency maps a center frequency in Hz to an amateur band name.
func bandForFrequency(hz float64) string {
	mhz := hz / 1e6
	switch {
	case mhz >= 1.8 && mhz <= 2.0:
		return "160m"
	case mhz >= 3.5 && mhz <= 4.0:
		return "80m"
	case mhz >= 5.330 && mhz <= 5.405:
		return "60m"
	case mhz >= 7.0 && mhz <= 7.3:
		return "40m"
	case mhz >= 10.1 && mhz <= 10.15:
		return "30m"
	case mhz >= 14.0 && mhz <= 14.35:
		return "20m"
	case mhz >= 18.068 && mhz <= 18.168:
		return "17m"
	case mhz >= 21.0 && mhz <= 21.45:
		return "15m"
	case mhz >= 24.89 && mhz <= 24.99:
		return "12m"
	case mhz >= 28.0 && mhz <= 29.7:
		return "10m"
	case mhz >= 50 && mhz <= 54:
		return "6m"
	case mhz >= 144 && mhz <= 148:
		return "2m"
	case mhz >= 222 && mhz <= 225:
		return "1.25m"
	case mhz >= 420 && mhz <= 450:
		return "70cm"
	case mhz >= 902 && mhz <= 928:
		return "33cm"
	case mhz >= 1240 && mhz <= 1300:
		return "23cm"
	default:
		if mhz < 30 {
			return "HF"
		}
		return "Other"
	}
}
