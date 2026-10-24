// Package safety checks link destinations against Google Safe Browsing
// (D23). Only link creation calls it; the redirect path never does.
package safety

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

var (
	// ErrUnsafe: Google lists the URL. Maps to 400 UNSAFE_URL.
	ErrUnsafe = errors.New("destination flagged by Safe Browsing")

	// ErrUnavailable: no answer, so creation fails closed. Maps to 503.
	ErrUnavailable = errors.New("safe browsing unavailable")
)

type Checker interface {
	Check(ctx context.Context, url string) error
}

// BatchChecker is the re-scan's view (D27): which of these URLs are listed.
type BatchChecker interface {
	CheckBatch(ctx context.Context, urls []string) (flagged map[string]string, err error)
}

// MaxBatch is the Lookup API's limit on URLs per request.
const MaxBatch = 500

// SafeBrowsing is a Lookup API v4 client. The endpoint is a parameter so tests
// can point it at a fake; main passes Google's (D18).
type SafeBrowsing struct {
	endpoint string
	apiKey   string
	client   *http.Client
}

const Endpoint = "https://safebrowsing.googleapis.com/v4/threatMatches:find"

func NewSafeBrowsing(endpoint, apiKey string, client *http.Client) *SafeBrowsing {
	return &SafeBrowsing{endpoint: endpoint, apiKey: apiKey, client: client}
}

var threatTypes = []string{"MALWARE", "SOCIAL_ENGINEERING", "UNWANTED_SOFTWARE", "POTENTIALLY_HARMFUL_APPLICATION"}

type threatEntry struct {
	URL string `json:"url"`
}

type findRequest struct {
	Client struct {
		ClientID      string `json:"clientId"`
		ClientVersion string `json:"clientVersion"`
	} `json:"client"`
	ThreatInfo struct {
		ThreatTypes      []string      `json:"threatTypes"`
		PlatformTypes    []string      `json:"platformTypes"`
		ThreatEntryTypes []string      `json:"threatEntryTypes"`
		ThreatEntries    []threatEntry `json:"threatEntries"`
	} `json:"threatInfo"`
}

type findResponse struct {
	Matches []struct {
		ThreatType string      `json:"threatType"`
		Threat     threatEntry `json:"threat"`
	} `json:"matches"`
}

func (s *SafeBrowsing) Check(ctx context.Context, u string) error {
	resp, err := s.find(ctx, []string{u})
	if err != nil {
		return err
	}
	if len(resp.Matches) > 0 {
		return ErrUnsafe
	}
	return nil
}

// CheckBatch maps each listed URL, exactly as sent, to its threat type.
func (s *SafeBrowsing) CheckBatch(ctx context.Context, urls []string) (map[string]string, error) {
	if len(urls) > MaxBatch {
		return nil, fmt.Errorf("safe browsing batch of %d URLs exceeds %d", len(urls), MaxBatch)
	}
	resp, err := s.find(ctx, urls)
	if err != nil {
		return nil, err
	}
	flagged := map[string]string{}
	for _, m := range resp.Matches {
		flagged[m.Threat.URL] = m.ThreatType
	}
	return flagged, nil
}

func (s *SafeBrowsing) find(ctx context.Context, urls []string) (findResponse, error) {
	var req findRequest
	req.Client.ClientID = "shortlink"
	req.Client.ClientVersion = "2"
	req.ThreatInfo.ThreatTypes = threatTypes
	req.ThreatInfo.PlatformTypes = []string{"ANY_PLATFORM"}
	req.ThreatInfo.ThreatEntryTypes = []string{"URL"}
	for _, u := range urls {
		req.ThreatInfo.ThreatEntries = append(req.ThreatInfo.ThreatEntries, threatEntry{URL: u})
	}
	body, err := json.Marshal(req)
	if err != nil {
		return findResponse{}, err
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
		s.endpoint+"?key="+url.QueryEscape(s.apiKey), bytes.NewReader(body))
	if err != nil {
		return findResponse{}, s.unavailable(err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := s.client.Do(httpReq)
	if err != nil {
		return findResponse{}, s.unavailable(err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, httpResp.Body)
		return findResponse{}, s.unavailable(fmt.Errorf("status %d", httpResp.StatusCode))
	}
	var out findResponse
	if err := json.NewDecoder(httpResp.Body).Decode(&out); err != nil {
		return findResponse{}, s.unavailable(fmt.Errorf("decoding response: %v", err))
	}
	return out, nil
}

// unavailable wraps a cause for the logs. Transport errors embed the request
// URL, which carries the key, so the key is scrubbed from the message.
func (s *SafeBrowsing) unavailable(cause error) error {
	msg := cause.Error()
	if s.apiKey != "" {
		msg = strings.ReplaceAll(msg, url.QueryEscape(s.apiKey), "REDACTED")
		msg = strings.ReplaceAll(msg, s.apiKey, "REDACTED")
	}
	return fmt.Errorf("%w: %s", ErrUnavailable, msg)
}
