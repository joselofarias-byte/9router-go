package entitlements

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
	"time"
)

const (
	defaultControlPlaneTimeout      = 15 * time.Second
	defaultControlPlaneResponseSize = int64(1 << 20)
)

var (
	ErrControlPlaneResponseTooLarge = errors.New("control plane response too large")
	ErrControlPlaneInvalidResponse  = errors.New("invalid control plane response")
)

type HTTPTransport struct {
	baseURL          string
	client           *http.Client
	maxResponseBytes int64
}

type httpLeaseResponse struct {
	Lease               json.RawMessage `json:"lease"`
	ServerTime          time.Time       `json:"server_time"`
	RenewalAfterSeconds int64           `json:"renewal_after_seconds,omitempty"`
}

func NewHTTPTransport(baseURL string, client *http.Client) (*HTTPTransport, error) {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		return nil, errors.New("entitlements.NewHTTPTransport: base URL is required")
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return nil, errors.New("entitlements.NewHTTPTransport: invalid base URL")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return nil, errors.New("entitlements.NewHTTPTransport: unsupported URL scheme")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return nil, errors.New("entitlements.NewHTTPTransport: base URL must not include query or fragment")
	}
	if client == nil {
		client = &http.Client{Timeout: defaultControlPlaneTimeout}
	}
	return &HTTPTransport{
		baseURL:          strings.TrimRight(baseURL, "/"),
		client:           client,
		maxResponseBytes: defaultControlPlaneResponseSize,
	}, nil
}

func (t *HTTPTransport) Activate(ctx context.Context, request ActivationRequest) (LeaseResponse, error) {
	return t.post(ctx, "/v1/activate", request)
}

func (t *HTTPTransport) Renew(ctx context.Context, request RenewalRequest) (LeaseResponse, error) {
	return t.post(ctx, "/v1/lease/renew", request)
}

func (t *HTTPTransport) post(ctx context.Context, path string, request any) (LeaseResponse, error) {
	if t == nil || t.client == nil || t.baseURL == "" {
		return LeaseResponse{}, ErrControlPlaneTransport
	}
	payload, err := json.Marshal(request)
	if err != nil {
		return LeaseResponse{}, fmt.Errorf("encode control plane request: %w", err)
	}

	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, t.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return LeaseResponse{}, fmt.Errorf("create control plane request: %w", err)
	}
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "application/json")

	response, err := t.client.Do(httpRequest)
	if err != nil {
		return LeaseResponse{}, fmt.Errorf("control plane request failed: %w", err)
	}
	defer response.Body.Close()

	limit := t.maxResponseBytes
	if limit <= 0 {
		limit = defaultControlPlaneResponseSize
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil {
		return LeaseResponse{}, fmt.Errorf("read control plane response: %w", err)
	}
	if int64(len(raw)) > limit {
		return LeaseResponse{}, ErrControlPlaneResponseTooLarge
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		// Deliberately do not include the upstream body: it may echo activation
		// codes, tokens or other credential-bearing request details.
		return LeaseResponse{}, fmt.Errorf("control plane request failed: HTTP %d %s", response.StatusCode, http.StatusText(response.StatusCode))
	}

	var wire httpLeaseResponse
	if err := json.Unmarshal(raw, &wire); err != nil {
		return LeaseResponse{}, fmt.Errorf("%w: %v", ErrControlPlaneInvalidResponse, err)
	}
	if len(wire.Lease) == 0 {
		return LeaseResponse{}, ErrControlPlaneInvalidResponse
	}
	return LeaseResponse{
		Lease:               append([]byte(nil), wire.Lease...),
		ServerTime:          wire.ServerTime.UTC(),
		RenewalAfterSeconds: wire.RenewalAfterSeconds,
	}, nil
}
