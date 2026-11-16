// Package voacap encodes VOACAP input files and parses VOACAP output.
//
// Adapted from the MIT-licensed Pat client by Martin Hebnes Pedersen
// (LA5NTA): https://github.com/la5nta/pat/internal/propagation/voacap.
// The upstream version parses Maidenhead locators via an external package;
// this copy takes latitude/longitude directly so the project stays
// dependency-free.
package voacap

import (
	"cmp"
	"fmt"
	"io"
	"math"
	"time"
)

// EncodeParams holds the input parameters for a VOACAP prediction.
type EncodeParams struct {
	DateTime           time.Time
	FromLat, FromLon   float64
	ToLat, ToLon       float64
	FromLabel, ToLabel string
	SSN                int
	TransmitPower      int
	MinSNR             int
	Frequency          float64 // MHz
	MinTakeoffAngle    float64 // degrees
	MultipathDelay     float64 // ms
	MultipathTolerance float64 // dB
	LocalNoise         float64 // -dBW/Hz at 3 MHz
}

// Encode writes a VOACAP input deck to w.
func Encode(w io.Writer, params EncodeParams) error {
	params.DateTime = params.DateTime.UTC()
	if params.FromLabel == "" {
		params.FromLabel = "LOCAL"
	}
	if params.ToLabel == "" {
		params.ToLabel = "REMOTE"
	}

	writeCommentCard(w, fmt.Sprintf("%.1f MHz, %s -> %s @ %s", params.Frequency, params.FromLabel, params.ToLabel, params.DateTime.Truncate(time.Second).Format(time.RFC3339)))
	writeLineMaxCard(w, 55)
	writeCoeffsCard(w, "CCIR")
	writeTimeCard(w, params.DateTime.Hour(), params.DateTime.Hour(), 1, 1)
	writeMonthCard(w, params.DateTime.Year(), params.DateTime.Month())
	writeSunspotCard(w, params.SSN)
	writeLabelCard(w, params.FromLabel, params.ToLabel)
	writeCircuitCard(w, params.FromLat, params.FromLon, params.ToLat, params.ToLon, 0)

	// Defaults from Pat's express-params.md when unspecified.
	minTakeoffAngle := params.MinTakeoffAngle
	if minTakeoffAngle == 0 {
		minTakeoffAngle = 15.0
	}
	multipathDelay := params.MultipathDelay
	if multipathDelay == 0 {
		multipathDelay = 0.1
	}
	multipathTolerance := params.MultipathTolerance
	if multipathTolerance == 0 {
		multipathTolerance = 0.5
	}
	localNoise := params.LocalNoise
	if localNoise == 0 {
		localNoise = 145.0
	}

	writeSystemCard(w, float64(params.MinSNR), minTakeoffAngle, multipathDelay, multipathTolerance, localNoise)
	writeFprobCard(w)
	writeAntennaCard(w, 1, 1, 2, 30, params.Frequency, "", 90.0, float64(params.TransmitPower))
	writeAntennaCard(w, 2, 2, 2, 30, params.Frequency, "", 270.0, float64(params.TransmitPower))
	writeFrequencyCard(w, params.Frequency)
	writeMethodCard(w, 30, 0)
	writeExecuteCard(w)
	writeQuitCard(w)
	return nil
}

func getLatHemi(lat float64) string {
	if lat < 0 {
		return "S"
	}
	return "N"
}

func getLonHemi(lon float64) string {
	if lon < 0 {
		return "W"
	}
	return "E"
}

func writeCommentCard(w io.Writer, comment string) {
	fmt.Fprintf(w, "COMMENT    %-74s\n", comment)
}

func writeLineMaxCard(w io.Writer, maxLines int) {
	fmt.Fprintf(w, "LINEMAX   %5d\n", maxLines)
}

func writeCoeffsCard(w io.Writer, coeffs string) {
	fmt.Fprintf(w, "COEFFS    %-4s\n", coeffs)
}

func writeTimeCard(w io.Writer, start, end, step, mode int) {
	start, end = cmp.Or(start, 24), cmp.Or(end, 24) // Midnight hour is represented as 24
	fmt.Fprintf(w, "TIME         %5d%5d%5d%5d\n", start, end, step, mode)
}

func writeMonthCard(w io.Writer, year int, month time.Month) {
	fmt.Fprintf(w, "MONTH     %5d%5.2f\n", year, float64(month))
}

func writeSunspotCard(w io.Writer, sunspotNumber int) {
	fmt.Fprintf(w, "SUNSPOT   %5.2f\n", float64(sunspotNumber))
}

func writeLabelCard(w io.Writer, txName, rxName string) {
	fmt.Fprintf(w, "LABEL     %-20s%-20s\n", txName, rxName)
}

func writeCircuitCard(w io.Writer, txLat, txLon, rxLat, rxLon float64, path int) {
	fmt.Fprintf(w, "CIRCUIT   %5.2f%s%9.2f%s%9.2f%s%9.2f%s  S %5d\n",
		math.Abs(txLat), getLatHemi(txLat),
		math.Abs(txLon), getLonHemi(txLon),
		math.Abs(rxLat), getLatHemi(rxLat),
		math.Abs(rxLon), getLonHemi(rxLon),
		path)
}

func writeSystemCard(w io.Writer, requiredSnr, minTakeoffAngle, multipathDelay, multipathTolerance, localNoise float64) {
	fmt.Fprintf(w, "SYSTEM    %5.2f%5.0f%5.2f%5.0f%5.2f%5.2f%5.2f\n", 1.0, localNoise, multipathDelay, minTakeoffAngle, requiredSnr, multipathTolerance, multipathDelay)
}

func writeFprobCard(w io.Writer) {
	fmt.Fprintf(w, "FPROB     %5.2f%5.2f%5.2f%5.2f\n", 1.0, 1.0, 1.0, 0.0)
}

func writeAntennaCard(w io.Writer, antType, id, minFreq, maxFreq int, freqMHz float64, antFile string, bearing, powerW float64) {
	powerKW := powerW / 1000.0
	fmt.Fprintf(w, "ANTENNA   %5d%5d%5d%5d%10.3f[%-21s]%5.1f%10.4f\n", antType, id, minFreq, maxFreq, freqMHz, antFile, bearing, powerKW)
}

func writeFrequencyCard(w io.Writer, freqMHz float64) {
	fmt.Fprintf(w, "FREQUENCY %5.3f\n", freqMHz)
}

func writeMethodCard(w io.Writer, method, spec int) {
	fmt.Fprintf(w, "METHOD    %5d%5d\n", method, spec)
}

func writeExecuteCard(w io.Writer) {
	fmt.Fprintln(w, "EXECUTE")
}

func writeQuitCard(w io.Writer) {
	fmt.Fprintln(w, "QUIT")
}
