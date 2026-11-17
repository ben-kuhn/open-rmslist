package cmsapi

import (
	"encoding/json"
	"errors"
	"fmt"
)

// GatewayStatus is the top-level JSON document returned by the Winlink
// gateway status endpoint (POST /gateway/status.json).
type GatewayStatus struct {
	ServerName     string          `json:"ServerName"`
	ErrorCode      int             `json:"ErrorCode"`
	ResponseStatus *ResponseStatus `json:"ResponseStatus"`
	Gateways       []Gateway       `json:"Gateways"`
}

// ResponseStatus is the ServiceStack error object returned for bad requests.
type ResponseStatus struct {
	ErrorCode string `json:"ErrorCode"`
	Message   string `json:"Message"`
}

// Gateway describes a single RMS gateway and its list of channels.
type Gateway struct {
	Timestamp        string           `json:"Timestamp"`
	Callsign         string           `json:"Callsign"`
	BaseCallsign     string           `json:"BaseCallsign"`
	Latitude         float64          `json:"Latitude"`
	Longitude        float64          `json:"Longitude"`
	HoursSinceStatus int              `json:"HoursSinceStatus"`
	LastStatus       string           `json:"LastStatus"`
	Comments         string           `json:"Comments"`
	RequestedMode    string           `json:"RequestedMode"`
	Channels         []GatewayChannel `json:"GatewayChannels"`
}

// GatewayChannel is a single (frequency, mode) channel of a gateway.
type GatewayChannel struct {
	Timestamp      string  `json:"Timestamp"`
	SupportedModes string  `json:"SupportedModes"`
	Mode           int     `json:"Mode"`
	Gridsquare     string  `json:"Gridsquare"`
	Frequency      float64 `json:"Frequency"`
	OperatingHours string  `json:"OperatingHours"`
	Baud           string  `json:"Baud"`
	RadioRange     string  `json:"RadioRange"`
	Antenna        string  `json:"Antenna"`
	ServiceCode    string  `json:"ServiceCode"`
}

// ValidateGatewayStatus rejects malformed or error documents before they can
// replace the catalog or poison the cache. A valid feed must contain at least
// one gateway with a callsign.
func ValidateGatewayStatus(b []byte) error {
	var st GatewayStatus
	if err := json.Unmarshal(b, &st); err != nil {
		return fmt.Errorf("invalid gateway JSON: %w", err)
	}
	if st.ResponseStatus != nil && st.ResponseStatus.ErrorCode != "" {
		return fmt.Errorf("Winlink error %s: %s", st.ResponseStatus.ErrorCode, st.ResponseStatus.Message)
	}
	if st.ErrorCode != 0 {
		return fmt.Errorf("Winlink error code %d", st.ErrorCode)
	}
	if len(st.Gateways) == 0 {
		return errors.New("gateway feed contains no gateways")
	}
	for _, g := range st.Gateways {
		if g.Callsign == "" {
			return errors.New("gateway feed contains a gateway without a callsign")
		}
	}
	return nil
}
