package cmsapi

// GatewayStatus is the top-level JSON document returned by the Winlink
// gateway status endpoint (POST /gateway/status.json).
type GatewayStatus struct {
	ServerName string    `json:"ServerName"`
	ErrorCode  int       `json:"ErrorCode"`
	Gateways   []Gateway `json:"Gateways"`
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
