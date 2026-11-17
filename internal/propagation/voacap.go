package propagation

// VOACAP point-to-point prediction engines.
//
// The encoder/parser (internal/propagation/voacap) and the Link Quality Index
// (LQI) scoring below are adapted from the MIT-licensed Pat client by Martin
// Hebnes Pedersen (LA5NTA), github.com/la5nta/pat/internal/propagation.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/ben-kuhn/rms-radar/internal/propagation/voacap"
)

// ErrFrequencyOutOfBounds is returned when a frequency is outside the range
// the VOACAP model supports (HF, < 30 MHz).
var ErrFrequencyOutOfBounds = errors.New("frequency out of bounds")

const (
	MinTakeoffAngle    = 3.0
	MultipathDelay     = 0.1
	MultipathTolerance = 0.5
	LocalNoise         = 145.0

	// LQI component weights (see Pat's voacap_adapter.go).
	WeightReliability  = 2.0
	WeightSNR          = 1.0
	WeightTakeoffAngle = 0.3
	WeightMUFday       = 0.2
	WeightMode         = 1.0
	WeightSDBW         = 0.6
)

// voacapLocal runs a locally installed voacapl/voacapw binary.
type voacapLocal struct {
	executable string
	dataDir    string
	mu         sync.Mutex // serialize voacapw (no --run-dir support)
}

// NewVOACAP returns a predictor that shells out to a local VOACAP executable.
func NewVOACAP(executable, dataDir string) (*voacapLocal, error) {
	executable, err := findExecutable(executable)
	if err != nil {
		return nil, err
	}
	if dataDir == "" {
		dataDir = defaultDataDir(executable)
	}
	if _, err := os.Stat(dataDir); err != nil {
		return nil, fmt.Errorf("voacap data directory: %w", err)
	}
	return &voacapLocal{executable: executable, dataDir: dataDir}, nil
}

func (p *voacapLocal) Available() bool { return p != nil && p.executable != "" }

func (p *voacapLocal) Predict(ctx context.Context, params Params) (*Prediction, error) {
	input, err := generateInputFile(params)
	if err != nil {
		return nil, err
	}

	const inputName, outputName = "input.dat", "output.out"

	runDir := filepath.Join(p.dataDir, "run")
	args := []string{p.dataDir, inputName, outputName}
	if isVoacapl(p.executable) {
		dir, err := os.MkdirTemp("", "rmslist-voacap-")
		if err != nil {
			return nil, err
		}
		defer os.RemoveAll(dir)
		runDir = dir
		args = append([]string{"--run-dir=" + runDir}, args...)
	} else {
		args = append([]string{"SILENT"}, args...)
		p.mu.Lock()
		defer p.mu.Unlock()
	}

	if err := os.WriteFile(filepath.Join(runDir, inputName), input, 0o644); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, p.executable, args...)
	cmd.Dir = runDir
	// Bundled engines ship their gfortran runtime next to the executable.
	if libDir := filepath.Join(filepath.Dir(p.executable), "lib"); runtime.GOOS != "windows" && dirExists(libDir) {
		key := "LD_LIBRARY_PATH"
		if runtime.GOOS == "darwin" {
			key = "DYLD_LIBRARY_PATH"
		}
		prefix := key + "="
		env := os.Environ()
		clean := make([]string, 0, len(env)+1)
		value := libDir
		for _, e := range env {
			if strings.HasPrefix(e, prefix) {
				if old := strings.TrimPrefix(e, prefix); old != "" {
					value = libDir + string(os.PathListSeparator) + old
				}
				continue
			}
			clean = append(clean, e)
		}
		cmd.Env = append(clean, prefix+value)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("voacap failed: %w\n%s", err, out)
	}

	f, err := os.Open(filepath.Join(runDir, outputName))
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return toPrediction(f, params)
}

// voacapAPI delegates to a remote VOACAP service (e.g. VOACAP Online).
type voacapAPI struct {
	apiURL string
	client *http.Client
}

// NewVOACAPAPI returns a predictor that POSTs the VOACAP input deck to a
// remote API endpoint exposing /predict.
func NewVOACAPAPI(apiURL string) *voacapAPI {
	return &voacapAPI{apiURL: strings.TrimRight(apiURL, "/"), client: &http.Client{Timeout: 30 * time.Second}}
}

func (p *voacapAPI) Available() bool { return p != nil && p.apiURL != "" }

func (p *voacapAPI) Predict(ctx context.Context, params Params) (*Prediction, error) {
	input, err := generateInputFile(params)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.apiURL+"/predict", bytes.NewReader(input))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "text/plain")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("voacap API %d: %s", resp.StatusCode, string(body))
	}
	return toPrediction(resp.Body, params)
}

func generateInputFile(params Params) ([]byte, error) {
	if params.FrequencyHz >= 30e6 {
		return nil, ErrFrequencyOutOfBounds
	}
	ssn := params.SSN
	if ssn <= 0 {
		ssn = 100 // mid-cycle fallback
	}
	var buf bytes.Buffer
	err := voacap.Encode(&buf, voacap.EncodeParams{
		DateTime:           time.Now(),
		FromLat:            params.FromLat,
		FromLon:            params.FromLon,
		ToLat:              params.ToLat,
		ToLon:              params.ToLon,
		FromLabel:          "LOCAL",
		ToLabel:            "REMOTE",
		SSN:                ssn,
		TransmitPower:      50,
		MinSNR:             20,
		Frequency:          params.FrequencyHz / 1e6,
		MinTakeoffAngle:    MinTakeoffAngle,
		MultipathDelay:     MultipathDelay,
		MultipathTolerance: MultipathTolerance,
		LocalNoise:         LocalNoise,
	})
	if err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func toPrediction(r io.Reader, params Params) (*Prediction, error) {
	const maxOutputBytes = 8 << 20
	raw, err := io.ReadAll(io.LimitReader(r, maxOutputBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxOutputBytes {
		return nil, fmt.Errorf("VOACAP output exceeds %d MiB", maxOutputBytes>>20)
	}
	out, err := voacap.Parse(bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}

	lqi, snr, rel, err := computeLQI(out, params.FrequencyHz/1e6)
	if err != nil {
		return nil, err
	}

	from := Point{Lat: params.FromLat, Lon: params.FromLon}
	to := Point{Lat: params.ToLat, Lon: params.ToLon}
	dist := DistanceKm(from, to)
	freqMHz := params.FrequencyHz / 1e6

	// Short paths are groundwave/NVIS, where VOACAP's skywave statistics
	// under-report reliability. Use its LQI as a floor, never a ceiling.
	skyLQI := lqi
	open := rel >= 0.5
	note := fmt.Sprintf("VOACAP LQI %d (reliability %.0f%%, SNR %.0f dB)", skyLQI, rel*100, snr)
	if gw := GroundwaveScore(dist, freqMHz); gw > lqi {
		lqi = gw
		open = true
		note = fmt.Sprintf(
			"VOACAP LQI %d, raised to %d%% for short-path groundwave/NVIS (reliability %.0f%%, SNR %.0f dB)",
			skyLQI, lqi, rel*100, snr)
	}

	return &Prediction{
		LinkQuality:  lqi,
		SNRdB:        snr,
		BandOpen:     open,
		FrequencyMHz: freqMHz,
		DistanceKm:   dist,
		BearingDeg:   BearingDeg(from, to),
		Method:       "voacap",
		Note:         note,
	}, nil
}

// computeLQI ports Pat's Link Quality Index: a 0-100 score combining path
// reliability, SNR, takeoff angle, MUFday, propagation mode (hops/layers) and
// signal power.
func computeLQI(out *voacap.VoacapOutput, freqMHz float64) (int, float64, float64, error) {
	var prediction *voacap.Prediction
	for i := range out.Predictions {
		p := &out.Predictions[i]
		if int(p.Hour)%24 == out.Request.Hour%24 {
			prediction = p
			break
		}
	}
	if prediction == nil {
		return 0, 0, 0, fmt.Errorf("no prediction found for hour %d", out.Request.Hour)
	}

	var band *voacap.BandPrediction
	var minFreqDiff float64
	for i := range prediction.BandPredictions {
		b := &prediction.BandPredictions[i]
		if math.IsNaN(b.SNR) {
			continue
		}
		d := math.Abs(b.Freq - freqMHz)
		if band == nil || d < minFreqDiff {
			band = b
			minFreqDiff = d
		}
	}
	if band == nil {
		return 0, 0, 0, fmt.Errorf("no usable prediction found in VOACAP output")
	}

	// Reliability factor (sigmoid around 85%).
	const (
		RelSigmoidSlope       = 3.0
		RelThreshold          = 0.85
		RelHighFactorMinValue = 0.73
		RelLowScaleFactor     = 1.0
	)
	relFactor := 0.0
	if band.Rel > 0.0 {
		switch {
		case band.Rel >= 0.999:
			relFactor = 1.0
		case band.Rel >= 0.95:
			relFactor = 0.95 + (0.98-0.95)*((band.Rel-0.95)/(0.999-0.95))
		case band.Rel >= RelThreshold:
			sig := 1.0 / (1.0 + math.Exp(-RelSigmoidSlope*(band.Rel-RelThreshold)))
			relFactor = RelHighFactorMinValue + (0.95-RelHighFactorMinValue)*sig
		default:
			sig := 1.0 / (1.0 + math.Exp(-RelSigmoidSlope*(band.Rel-RelThreshold)))
			relFactor = sig * RelLowScaleFactor
		}
	}

	// SNR factor.
	const (
		SNRMinimumThreshold = 3.0
		SNRMaximumThreshold = 60.0
	)
	snrFactor := 0.0
	if band.SNR >= SNRMinimumThreshold {
		if band.SNR >= SNRMaximumThreshold {
			snrFactor = 1.0
		} else {
			snrFactor = (band.SNR - SNRMinimumThreshold) / (SNRMaximumThreshold - SNRMinimumThreshold)
		}
	}

	// Takeoff-angle factor (penalizes wide angles; very low angles are good).
	const (
		LowAngleThreshold = 30.0
		MaxAnglePenalty   = 0.5
	)
	angleFactor := 0.0
	if band.Tangle > 0 && band.Tangle <= 90.0 {
		tangle := band.Tangle
		if tangle < MinTakeoffAngle {
			// VOACAP can report an angle below the requested minimum; a lower
			// angle is not a failure, so score it at the requested minimum
			// instead of zeroing the whole prediction.
			tangle = MinTakeoffAngle
		}
		if tangle >= LowAngleThreshold {
			angleFactor = 1.0
		} else {
			norm := (tangle - MinTakeoffAngle) / (LowAngleThreshold - MinTakeoffAngle)
			angleFactor = MaxAnglePenalty + norm*(1.0-MaxAnglePenalty)
		}
	}

	// MUFday factor (with a floor for currently-good bands).
	const MUFdayFloor = 0.3
	mufFactor := 0.0
	if band.MUFday >= 0.0 && band.MUFday <= 1.0 {
		mufFactor = band.MUFday
		if mufFactor < MUFdayFloor {
			mufFactor = MUFdayFloor
		}
	}

	// Propagation mode factor (number of hops and ionospheric layer).
	modeFactor := 0.0
	if band.Mode != "" {
		if len(band.Mode) >= 2 && band.Mode[0] >= '0' && band.Mode[0] <= '9' {
			hops := int(band.Mode[0] - '0')
			var layer string
			if len(band.Mode) >= 3 && band.Mode[1] == ' ' {
				layer = band.Mode[2:]
			} else {
				layer = band.Mode[1:]
			}
			switch hops {
			case 1:
				switch layer {
				case "F2":
					modeFactor = 1.0
				case "F1":
					modeFactor = 0.9
				case "E":
					modeFactor = 0.8
				default:
					modeFactor = 0.7
				}
			case 2:
				switch layer {
				case "F2":
					modeFactor = 0.7
				case "F1":
					modeFactor = 0.6
				case "E":
					modeFactor = 0.5
				default:
					modeFactor = 0.4
				}
			default:
				modeFactor = 0.3
			}
		} else if len(band.Mode) >= 4 {
			switch {
			case band.Mode[:2] == "F2" && band.Mode[2:4] == "F2":
				modeFactor = 0.5
			case band.Mode[:2] == "F2" || band.Mode[2:4] == "F2":
				modeFactor = 0.4
			default:
				modeFactor = 0.3
			}
		} else {
			modeFactor = 0.3
		}
	} else {
		modeFactor = 0.5
	}

	// Signal power (S_DBW) factor.
	const (
		SDBWMinThreshold = -163.0
		SDBWMaxThreshold = -63.0
		SDBWRange        = SDBWMaxThreshold - SDBWMinThreshold
		SDBWFloor        = 0.05
	)
	sdbwFactor := 0.0
	switch {
	case band.SDBW > -200.0 && band.SDBW <= SDBWMinThreshold:
		sdbwFactor = SDBWFloor
	case band.SDBW > SDBWMinThreshold:
		if band.SDBW >= SDBWMaxThreshold {
			sdbwFactor = 1.0
		} else {
			sdbwFactor = math.Max(math.Sqrt((band.SDBW-SDBWMinThreshold)/SDBWRange), SDBWFloor)
		}
	}

	lqi := math.Pow(relFactor, WeightReliability) *
		math.Pow(snrFactor, WeightSNR) *
		math.Pow(angleFactor, WeightTakeoffAngle) *
		math.Pow(mufFactor, WeightMUFday) *
		math.Pow(modeFactor, WeightMode) *
		math.Pow(sdbwFactor, WeightSDBW) * 100

	return int(math.Round(lqi)), band.SNR, band.Rel, nil
}

func defaultDataDir(executable string) string {
	var candidates []string
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "itshfbc"))
	}
	if executable != "" {
		// Look relative to the resolved binary: handles Nix/store, Homebrew, and
		// other prefix installs where the itshfbc data lives next to the binary.
		root := filepath.Dir(filepath.Dir(executable))
		candidates = append(candidates,
			filepath.Join(root, "itshfbc"),
			filepath.Join(root, "share", "voacapl", "itshfbc"),
			filepath.Join(root, "share", "itshfbc"),
		)
	}
	candidates = append(candidates,
		"/usr/share/voacapl/itshfbc",
		"/usr/local/share/voacapl/itshfbc",
		"/opt/homebrew/share/voacapl/itshfbc",
		"/opt/homebrew/opt/voacapl/share/voacapl/itshfbc",
		"/usr/local/opt/voacapl/share/voacapl/itshfbc",
	)
	if runtime.GOOS == "windows" {
		candidates = append(candidates, `C:\itshfbc`, `C:\itshfbc\data`)
	}
	for _, d := range candidates {
		if st, err := os.Stat(d); err == nil && st.IsDir() {
			return d
		}
	}
	return ""
}

func dirExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// Detect locates a VOACAP engine in the standard installation locations for
// the current platform. It returns empty strings when none is found.
func Detect() (string, string) {
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		candidates = []string{
			`C:\itshfbc\bin_win\voacapw.exe`,
			`C:\itshfbc\bin\voacapw.exe`,
		}
	default:
		candidates = []string{"voacapl", "/usr/local/bin/voacapl", "/opt/homebrew/bin/voacapl"}
		if home, err := os.UserHomeDir(); err == nil {
			candidates = append(candidates, filepath.Join(home, ".local", "bin", "voacapl"))
		}
	}
	for _, c := range candidates {
		exe, err := findExecutable(c)
		if err != nil {
			continue
		}
		if data := defaultDataDir(exe); data != "" {
			return exe, data
		}
	}
	return "", ""
}

// findExecutable resolves an executable name to an absolute path (absolute
// path, relative path, or PATH lookup).
func findExecutable(name string) (string, error) {
	name = os.ExpandEnv(name)
	if filepath.IsAbs(name) {
		if _, err := os.Stat(name); err != nil {
			return "", err
		}
		return name, nil
	}
	if strings.ContainsRune(name, os.PathSeparator) {
		abs, err := filepath.Abs(name)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", err
		}
		return abs, nil
	}
	return exec.LookPath(name)
}

func isVoacapl(executable string) bool {
	return filepath.Base(executable) == "voacapl"
}
