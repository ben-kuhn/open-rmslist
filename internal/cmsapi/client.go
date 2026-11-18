package cmsapi

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	// RootURL is the base URL of the Winlink CMS web services.
	RootURL = "https://api.winlink.org"
	// PathGatewayStatus returns the live gateway/channel status feed.
	PathGatewayStatus = "/gateway/status.json"

	// DefaultAccessKey is a Winlink web-service access key issued to the
	// open-source Pat client (Martin Hebnes Pedersen, LA5NTA) by the Winlink
	// Development Team in December 2017, for general community use. It is
	// published in Pat's MIT-licensed source at github.com/la5nta/pat.
	// We reuse the same public endpoint and key; override with -key if needed.
	DefaultAccessKey = "1880278F11684B358F36845615BD039A"
)

// Client talks to the Winlink CMS gateway status endpoint.
type Client struct {
	HTTP *http.Client
	Key  string

	baseURL string // defaults to RootURL; overridable in tests
}

// NewClient returns a client using key, falling back to DefaultAccessKey.
func NewClient(key string) *Client {
	if key == "" {
		key = DefaultAccessKey
	}
	return &Client{
		HTTP:    &http.Client{Timeout: 90 * time.Second},
		Key:     key,
		baseURL: RootURL,
	}
}

// Fetch downloads the gateway status feed for the given service codes.
// historyHours is clamped to the range [1, 48].
func (c *Client) Fetch(ctx context.Context, serviceCodes []string, historyHours int) ([]byte, error) {
	if len(serviceCodes) == 0 {
		serviceCodes = []string{"PUBLIC"}
	}
	if historyHours <= 0 || historyHours > 48 {
		historyHours = 48
	}

	form := url.Values{
		"Mode":         {"AnyAll"},
		"key":          {c.Key},
		"HistoryHours": {fmt.Sprint(historyHours)},
	}
	for _, sc := range serviceCodes {
		form.Add("ServiceCodes", sc)
	}

	base := c.baseURL
	if base == "" {
		base = RootURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+PathGatewayStatus, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "rms-radar (+https://github.com/ben-kuhn/rms-radar)")
	req.Header.Set("Accept", "application/json")

	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("gateway status: unexpected HTTP status %s", resp.Status)
	}
	const maxGatewayStatusBytes = 32 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxGatewayStatusBytes+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxGatewayStatusBytes {
		return nil, fmt.Errorf("gateway status: response exceeds %d MiB", maxGatewayStatusBytes>>20)
	}
	if err := ValidateGatewayStatus(b); err != nil {
		return nil, err
	}
	return b, nil
}
