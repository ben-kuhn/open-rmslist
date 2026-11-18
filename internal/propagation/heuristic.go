package propagation

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ben-kuhn/rms-radar/internal/atomicfile"
)

// maxSoundingAge bounds how old a cached condition/sounding may be before it is
// treated as stale.
const maxSoundingAge = 6 * time.Hour

// Conditions holds cached solar-terrestrial data used by the heuristic.
type Conditions struct {
	Updated  time.Time `json:"updated"`
	SFI      int       `json:"sfi"`
	AIndex   int       `json:"a_index"`
	KIndex   int       `json:"k_index"`
	Sunspots int       `json:"sunspots"`
	Stale    bool      `json:"stale"`
}

// Ionosonde is a single real-time ionosonde sounding.
type Ionosonde struct {
	Fof2 float64   `json:"fof2"`
	Mufd float64   `json:"mufd"`
	Lat  float64   `json:"lat"`
	Lon  float64   `json:"lon"`
	Time time.Time `json:"time"`
}

// Heuristic estimates link success using real-time ionosonde MUF data plus
// solar indices. It is always available: when the network is down it falls
// back to the last cached conditions, and when those are absent it degrades to
// a distance/band based estimate. This makes it the fallback when VOACAP is
// unavailable.
type Heuristic struct {
	mu         sync.RWMutex
	cond       Conditions
	ionosondes []Ionosonde
	cacheDir   string
	client     *http.Client
}

func NewHeuristic(cacheDir string) *Heuristic {
	h := &Heuristic{cacheDir: cacheDir, client: &http.Client{Timeout: 30 * time.Second}}
	// Warm the cache from disk so ranking works offline immediately.
	if c, i, err := h.loadCached(); err == nil {
		h.mu.Lock()
		h.cond, h.ionosondes = c, i
		h.mu.Unlock()
	}
	return h
}

func (h *Heuristic) Available() bool { return true }

// Conditions returns the current cached conditions.
func (h *Heuristic) Conditions() Conditions {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.cond
}

// RefreshConditions fetches fresh solar + ionosonde data and caches it. On
// failure it falls back to the on-disk cache and marks it stale.
func (h *Heuristic) RefreshConditions(ctx context.Context) {
	cond, iono, err := h.fetchConditions(ctx)
	if err != nil {
		if c, i, cerr := h.loadCached(); cerr == nil {
			log.Printf("propagation conditions refresh failed (%v); using cached data", err)
			c.Stale = true
			h.mu.Lock()
			h.cond, h.ionosondes = c, i
			h.mu.Unlock()
		} else {
			log.Printf("propagation conditions refresh failed (%v); no cache available", err)
		}
		return
	}
	cond.Updated = time.Now()
	h.mu.Lock()
	h.cond, h.ionosondes = cond, iono
	h.mu.Unlock()
	h.saveCached(cond, iono)
}

// Predict implements Predictor.
func (h *Heuristic) Predict(ctx context.Context, p Params) (*Prediction, error) {
	from := Point{Lat: p.FromLat, Lon: p.FromLon}
	to := Point{Lat: p.ToLat, Lon: p.ToLon}
	dist := DistanceKm(from, to)
	bearing := BearingDeg(from, to)
	freqMHz := p.FrequencyHz / 1e6

	pred := &Prediction{
		DistanceKm:   dist,
		BearingDeg:   bearing,
		FrequencyMHz: freqMHz,
		Method:       "heuristic",
	}

	h.mu.RLock()
	cond := h.cond
	h.mu.RUnlock()

	// VHF/UHF: line-of-sight, using the gateway's advertised radio range.
	if freqMHz >= 30 {
		pred.BandOpen = true
		pred.LinkQuality = vhfScore(dist, p.RadioRangeKm)
		if dist > 300 {
			pred.Note = "beyond typical VHF/UHF range"
		}
		return pred, nil
	}

	// HF: ionospheric propagation.
	muf, ok := h.pathMUF(from, to)
	if !ok {
		pred.LinkQuality = neutralHFScore(dist, freqMHz, cond)
		pred.Note = "MUF unavailable (no ionosonde data); distance-based estimate"
		applyGroundwaveFloor(pred, dist, freqMHz)
		return pred, nil
	}
	pred.MUF = muf
	pred.BandOpen = freqMHz <= muf
	pred.LinkQuality = hfScore(freqMHz, muf, dist, cond)
	applyGroundwaveFloor(pred, dist, freqMHz)
	return pred, nil
}

// pathMUF estimates the maximum usable frequency for a path from the current
// ionosonde soundings, ramping linearly from the vertical foF2 (0 km) toward
// the 3000 km MUF (mufd). Soundings are weighted by distance to the path
// midpoint with a gentle decay (no hard cutoff), so a global-average estimate
// is still produced when there are no soundings close to the path.
func (h *Heuristic) pathMUF(from, to Point) (float64, bool) {
	h.mu.RLock()
	iono := h.ionosondes
	h.mu.RUnlock()
	if len(iono) == 0 {
		return 0, false
	}
	mid := Point{Lat: (from.Lat + to.Lat) / 2, Lon: midpointLon(from.Lon, to.Lon)}
	dist := DistanceKm(from, to)

	now := time.Now()
	var sumF, sumM, wSum float64
	for _, s := range iono {
		if !s.Time.IsZero() && now.Sub(s.Time) > maxSoundingAge {
			continue
		}
		d := DistanceKm(mid, Point{Lat: s.Lat, Lon: s.Lon})
		w := 1.0 / (1.0 + d/800.0)
		sumF += s.Fof2 * w
		sumM += s.Mufd * w
		wSum += w
	}
	if wSum == 0 {
		return 0, false
	}
	fof2 := sumF / wSum
	mufd := sumM / wSum
	t := math.Min(1.0, dist/3000.0)
	return fof2 + (mufd-fof2)*t, true
}

// ---- scoring ----

func vhfScore(distKm, rangeKm float64) int {
	if rangeKm <= 0 {
		rangeKm = 80 // conservative default for a typical VHF/UHF data gateway
	}
	switch {
	case distKm <= rangeKm*0.5:
		return 90
	case distKm <= rangeKm:
		return clamp(90-int((distKm-rangeKm*0.5)/(rangeKm*0.5)*50), 0, 100)
	case distKm <= rangeKm*1.5:
		return clamp(40-int((distKm-rangeKm)/(rangeKm*0.5)*35), 0, 100)
	default:
		return clamp(5-int(distKm/rangeKm), 0, 100)
	}
}

func hfScore(freqMHz, mufMHz, distKm float64, c Conditions) int {
	if mufMHz <= 0 {
		return neutralHFScore(distKm, freqMHz, c)
	}
	margin := mufMHz / freqMHz
	if margin < 1 {
		// Band closed; small score that climbs as we approach the MUF.
		return clamp(int(20*margin), 0, 20)
	}
	base := 60.0
	base += math.Min(30.0, (margin-1)*40.0)

	solar := 1.0
	if c.SFI > 0 {
		solar = 0.6 + 0.5*clampF((float64(c.SFI)-70)/130.0, 0, 1)
	}
	switch {
	case c.KIndex >= 4:
		solar *= 0.7
	case c.KIndex >= 3:
		solar *= 0.9
	}

	distFactor := 1.0
	switch {
	case distKm < 100:
		// Groundwave/NVIS. Low bands are strong close in; high bands are in
		// the skip zone, where only weak groundwave is available.
		switch {
		case freqMHz <= 10.5:
			distFactor = 1.0
		case freqMHz <= 14.5:
			distFactor = 0.8
		case freqMHz <= 21.5:
			distFactor = 0.6
		default:
			distFactor = 0.45
		}
	case distKm > 4000:
		distFactor = 0.7
	case distKm > 2000:
		distFactor = 0.85
	}
	return clamp(int(base*solar*distFactor), 0, 100)
}

func neutralHFScore(distKm, freqMHz float64, c Conditions) int {
	base := 50.0
	switch {
	case freqMHz < 10:
		base += 10
	case freqMHz > 21:
		base -= 15
	}
	switch {
	case distKm < 300:
		base += 15
	case distKm > 3000:
		base -= 25
	}
	return clamp(int(base), 0, 100)
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func clampF(v, lo, hi float64) float64 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ---- data fetching ----

type hamqslSolar struct {
	XMLName   xml.Name `xml:"solar"`
	Solardata struct {
		Solarflux string `xml:"solarflux"`
		Aindex    string `xml:"aindex"`
		Kindex    string `xml:"kindex"`
		Sunspots  string `xml:"sunspots"`
	} `xml:"solardata"`
}

type kc2gStation struct {
	Fof2    float64 `json:"fof2"`
	Mufd    float64 `json:"mufd"`
	Time    string  `json:"time"`
	Station struct {
		Latitude  string `json:"latitude"`
		Longitude string `json:"longitude"`
		Name      string `json:"name"`
	} `json:"station"`
}

func (h *Heuristic) fetchConditions(ctx context.Context) (Conditions, []Ionosonde, error) {
	var cond Conditions
	var iono []Ionosonde

	solarErr := error(nil)
	if c, err := h.fetchSolar(ctx); err == nil {
		cond = c
	} else {
		solarErr = err
	}
	ionoErr := error(nil)
	if i, err := h.fetchIonosondes(ctx); err == nil {
		iono = i
	} else {
		ionoErr = err
	}

	switch {
	case solarErr != nil && ionoErr != nil:
		return cond, nil, fmt.Errorf("solar: %v; ionosonde: %v", solarErr, ionoErr)
	case solarErr != nil:
		log.Printf("propagation: solar fetch failed (%v); using ionosonde data only", solarErr)
	case ionoErr != nil:
		log.Printf("propagation: ionosonde fetch failed (%v); using solar data only", ionoErr)
	}
	cond.Updated = time.Now()
	return cond, iono, nil
}

func (h *Heuristic) fetchSolar(ctx context.Context) (Conditions, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://www.hamqsl.com/solarxml.php", nil)
	if err != nil {
		return Conditions{}, err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return Conditions{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return Conditions{}, fmt.Errorf("solar: HTTP %s", resp.Status)
	}
	const maxSolarBytes = 2 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxSolarBytes+1))
	if err != nil {
		return Conditions{}, err
	}
	if len(b) > maxSolarBytes {
		return Conditions{}, fmt.Errorf("solar: response exceeds %d MiB", maxSolarBytes>>20)
	}
	var s hamqslSolar
	if err := xml.Unmarshal(b, &s); err != nil {
		return Conditions{}, err
	}
	return Conditions{
		SFI:      atoiSafe(s.Solardata.Solarflux),
		AIndex:   atoiSafe(s.Solardata.Aindex),
		KIndex:   atoiSafe(s.Solardata.Kindex),
		Sunspots: atoiSafe(s.Solardata.Sunspots),
	}, nil
}

func (h *Heuristic) fetchIonosondes(ctx context.Context) ([]Ionosonde, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://prop.kc2g.com/api/stations.json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ionosondes: HTTP %s", resp.Status)
	}
	const maxIonoBytes = 8 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxIonoBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxIonoBytes {
		return nil, fmt.Errorf("ionosondes: response exceeds %d MiB", maxIonoBytes>>20)
	}
	var raw []kc2gStation
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	now := time.Now()
	var out []Ionosonde
	for _, s := range raw {
		if s.Fof2 <= 0 || s.Mufd <= 0 {
			continue
		}
		lat, err1 := strconv.ParseFloat(strings.TrimSpace(s.Station.Latitude), 64)
		lon, err2 := strconv.ParseFloat(strings.TrimSpace(s.Station.Longitude), 64)
		if err1 != nil || err2 != nil {
			continue
		}
		if lon > 180 {
			lon -= 360
		}
		t, err := parseKC2GTime(s.Time)
		if err != nil {
			continue
		}
		if now.Sub(t).Abs() > 2*time.Hour {
			continue
		}
		out = append(out, Ionosonde{Fof2: s.Fof2, Mufd: s.Mufd, Lat: lat, Lon: lon, Time: t})
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no fresh ionosonde data")
	}
	return out, nil
}

func atoiSafe(s string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(s))
	return n
}

// parseKC2GTime parses the prop.kc2g.com timestamp, which is usually UTC with
// no timezone suffix (e.g. "2026-11-16T20:15:01"), but may occasionally carry
// one or include fractional seconds.
func parseKC2GTime(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	for _, layout := range []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02T15:04:05.999999999",
		"2006-01-02T15:04:05",
	} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("unrecognized time %q", s)
}

// ---- caching ----

type conditionsFile struct {
	Conditions Conditions  `json:"conditions"`
	Ionosondes []Ionosonde `json:"ionosondes"`
}

func (h *Heuristic) cachePath() string {
	return filepath.Join(h.cacheDir, "conditions.json")
}

func (h *Heuristic) saveCached(c Conditions, iono []Ionosonde) {
	if h.cacheDir == "" {
		return
	}
	b, err := json.Marshal(conditionsFile{Conditions: c, Ionosondes: iono})
	if err != nil {
		return
	}
	if err := atomicfile.Write(h.cachePath(), b, 0o644); err != nil {
		log.Printf("conditions cache write: %v", err)
	}
}

func (h *Heuristic) loadCached() (Conditions, []Ionosonde, error) {
	if h.cacheDir == "" {
		return Conditions{}, nil, fmt.Errorf("no cache directory")
	}
	b, err := os.ReadFile(h.cachePath())
	if err != nil {
		return Conditions{}, nil, err
	}
	var cf conditionsFile
	if err := json.Unmarshal(b, &cf); err != nil {
		return Conditions{}, nil, err
	}
	if cf.Conditions.Updated.IsZero() || time.Since(cf.Conditions.Updated) > maxSoundingAge {
		cf.Conditions.Stale = true
	}
	return cf.Conditions, cf.Ionosondes, nil
}
