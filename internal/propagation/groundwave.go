package propagation

// Short HF paths are dominated by groundwave and near-vertical-incidence
// skywave (NVIS), not by long-haul F2 skywave. Skywave models are not
// meaningful there: VOACAP's MUFday statistic (the fraction of days the MUF
// exceeds the operating frequency) collapses toward zero for a few-kilometre
// path, so the composite Link Quality Index under-reports what is in practice a
// very reliable, essentially line-of-sight link. GroundwaveScore supplies a
// band-aware floor for those paths.

// groundwaveReach reports how far (km) groundwave remains useful on the band
// containing freqMHz, and the link quality at very short range. Higher
// frequencies are absorbed faster, so their reach and quality are lower.
func groundwaveReach(freqMHz float64) (reachKm, nearScore float64) {
	switch {
	case freqMHz <= 2.1: // 160 m
		return 120, 90
	case freqMHz <= 4.1: // 80 m
		return 90, 95
	case freqMHz <= 5.5: // 60 m
		return 60, 93
	case freqMHz <= 7.5: // 40 m
		return 45, 92
	case freqMHz <= 10.5: // 30 m
		return 30, 82
	case freqMHz <= 14.5: // 20 m
		return 20, 65
	case freqMHz <= 18.5: // 17 m
		return 12, 50
	case freqMHz <= 21.5: // 15 m
		return 9, 40
	case freqMHz <= 25.5: // 12 m
		return 6, 30
	default: // 10 m (still HF here)
		return 5, 24
	}
}

// GroundwaveScore estimates HF link quality for a path short enough to be
// dominated by groundwave/NVIS. It tapers to 0 by twice the band's groundwave
// reach, so callers can use it as a floor without affecting longer skywave
// paths.
func GroundwaveScore(distKm, freqMHz float64) int {
	reach, near := groundwaveReach(freqMHz)
	if distKm <= reach {
		return int(near)
	}
	if distKm >= 2*reach {
		return 0
	}
	frac := (distKm - reach) / reach // 0..1
	return clamp(int(near*(1-frac)), 0, int(near))
}

// applyGroundwaveFloor raises a short HF path's score to its groundwave/NVIS
// potential. It never lowers a score, so it only corrects paths where skywave
// statistics under-report a close-in link.
func applyGroundwaveFloor(p *Prediction, distKm, freqMHz float64) {
	gw := GroundwaveScore(distKm, freqMHz)
	if gw <= p.LinkQuality {
		return
	}
	p.LinkQuality = gw
	p.BandOpen = true
	if p.Note == "" {
		p.Note = "groundwave/NVIS (short path)"
	} else {
		p.Note += "; groundwave/NVIS floor"
	}
}
