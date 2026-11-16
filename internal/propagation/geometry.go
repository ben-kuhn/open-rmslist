package propagation

import (
	"errors"
	"math"
	"strings"
)

// Point is a latitude/longitude in decimal degrees.
type Point struct {
	Lat, Lon float64
}

// ParseLocator converts a Maidenhead grid locator (2, 4, or 6 characters,
// case-insensitive) into the center point of that square.
func ParseLocator(loc string) (Point, error) {
	loc = strings.ToUpper(strings.TrimSpace(loc))
	n := len(loc)
	if n < 2 || n%2 != 0 {
		return Point{}, errors.New("invalid grid locator length")
	}
	if n > 6 {
		n = 6 // ignore finer precision than a sub-square
	}
	if loc[0] < 'A' || loc[0] > 'R' || loc[1] < 'A' || loc[1] > 'R' {
		return Point{}, errors.New("invalid grid locator field")
	}
	if n >= 4 {
		if loc[2] < '0' || loc[2] > '9' || loc[3] < '0' || loc[3] > '9' {
			return Point{}, errors.New("invalid grid locator square")
		}
	}
	if n >= 6 {
		if loc[4] < 'A' || loc[4] > 'X' || loc[5] < 'A' || loc[5] > 'X' {
			return Point{}, errors.New("invalid grid locator subsquare")
		}
	}

	// First pair: field (20° lon x 10° lat).
	lon := float64(loc[0]-'A') * 20.0
	lat := float64(loc[1]-'A') * 10.0
	lon -= 180
	lat -= 90

	if n >= 4 {
		lon += float64(loc[2]-'0') * 2.0
		lat += float64(loc[3]-'0') * 1.0
	}
	if n >= 6 {
		lon += float64(loc[4]-'A') * (5.0 / 60.0)
		lat += float64(loc[5]-'A') * (2.5 / 60.0)
	}

	// Shift to the center of the (sub)square.
	switch n {
	case 2:
		lon += 10.0
		lat += 5.0
	case 4:
		lon += 1.0
		lat += 0.5
	default:
		lon += (5.0 / 60.0) / 2.0
		lat += (2.5 / 60.0) / 2.0
	}
	return Point{Lat: lat, Lon: lon}, nil
}

const earthRadiusKm = 6371.0

// DistanceKm returns the great-circle distance between two points in km.
func DistanceKm(a, b Point) float64 {
	phi1 := a.Lat * math.Pi / 180
	phi2 := b.Lat * math.Pi / 180
	dPhi := (b.Lat - a.Lat) * math.Pi / 180
	dLam := (b.Lon - a.Lon) * math.Pi / 180
	s := math.Sin(dPhi/2)*math.Sin(dPhi/2) +
		math.Cos(phi1)*math.Cos(phi2)*math.Sin(dLam/2)*math.Sin(dLam/2)
	return 2 * earthRadiusKm * math.Asin(math.Sqrt(s))
}

// BearingDeg returns the initial bearing from a to b in degrees (0-360).
func BearingDeg(a, b Point) float64 {
	phi1 := a.Lat * math.Pi / 180
	phi2 := b.Lat * math.Pi / 180
	dLam := (b.Lon - a.Lon) * math.Pi / 180
	y := math.Sin(dLam) * math.Cos(phi2)
	x := math.Cos(phi1)*math.Sin(phi2) - math.Sin(phi1)*math.Cos(phi2)*math.Cos(dLam)
	return math.Mod(math.Atan2(y, x)*180/math.Pi+360, 360)
}

// midpointLon returns the longitude halfway between two longitudes, handling
// the antimeridian.
func midpointLon(a, b float64) float64 {
	d := b - a
	switch {
	case d > 180:
		d -= 360
	case d < -180:
		d += 360
	}
	m := a + d/2
	switch {
	case m > 180:
		m -= 360
	case m < -180:
		m += 360
	}
	return m
}
