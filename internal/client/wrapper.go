package client

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Options configure an API client.
type Options struct {
	// URL is the Entrepot base URL (e.g. https://entrepot.example.com).
	URL string
	// Token is an Entrepot API token, OIDC access token or bootstrap token.
	Token              string
	CACertPEM          string
	InsecureSkipVerify bool
	Timeout            time.Duration
	UserAgent          string
}

// API wraps the generated client.
type API struct {
	*ClientWithResponses
	BaseURL string
}

// New returns an authenticated client for the /api/v1 management API.
func New(o Options) (*API, error) {
	base := strings.TrimRight(o.URL, "/")
	if base == "" {
		return nil, errors.New("url is required")
	}
	if !strings.HasPrefix(base, "http://") && !strings.HasPrefix(base, "https://") {
		return nil, fmt.Errorf("url %q must start with http:// or https://", o.URL)
	}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if o.CACertPEM != "" || o.InsecureSkipVerify {
		tlsCfg := &tls.Config{InsecureSkipVerify: o.InsecureSkipVerify} //nolint:gosec // explicit opt-in
		if o.CACertPEM != "" {
			pool, err := x509.SystemCertPool()
			if err != nil || pool == nil {
				pool = x509.NewCertPool()
			}
			if !pool.AppendCertsFromPEM([]byte(o.CACertPEM)) {
				return nil, errors.New("ca_cert_pem contains no valid certificates")
			}
			tlsCfg.RootCAs = pool
		}
		tr.TLSClientConfig = tlsCfg
	}
	timeout := o.Timeout
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	hc := &http.Client{Timeout: timeout, Transport: &retryTransport{next: tr, attempts: 4}}
	ua := o.UserAgent
	if ua == "" {
		ua = "terraform-provider-entrepot"
	}
	c, err := NewClientWithResponses(base+"/api/v1", WithHTTPClient(hc),
		WithRequestEditorFn(func(_ context.Context, req *http.Request) error {
			if o.Token != "" {
				req.Header.Set("Authorization", "Bearer "+o.Token)
			}
			req.Header.Set("User-Agent", ua)
			return nil
		}))
	if err != nil {
		return nil, err
	}
	return &API{ClientWithResponses: c, BaseURL: base}, nil
}

// retryTransport retries idempotent requests on 429/502/503/504 and
// connection errors with exponential backoff.
type retryTransport struct {
	next     http.RoundTripper
	attempts int
}

func (t *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	idempotent := req.Method == http.MethodGet || req.Method == http.MethodHead ||
		req.Method == http.MethodPut || req.Method == http.MethodDelete
	var lastErr error
	for i := 0; i < t.attempts; i++ {
		if i > 0 {
			if !idempotent {
				break
			}
			if req.GetBody != nil {
				b, err := req.GetBody()
				if err != nil {
					return nil, err
				}
				req.Body = b
			}
			select {
			case <-req.Context().Done():
				return nil, req.Context().Err()
			case <-time.After(time.Duration(250*(1<<(i-1))) * time.Millisecond):
			}
		}
		resp, err := t.next.RoundTrip(req)
		if err != nil {
			lastErr = err
			continue
		}
		switch resp.StatusCode {
		case http.StatusTooManyRequests, http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
			if idempotent && i < t.attempts-1 {
				resp.Body.Close()
				lastErr = fmt.Errorf("server returned %d", resp.StatusCode)
				continue
			}
		}
		return resp, nil
	}
	return nil, lastErr
}

// APIError is a non-success API response.
type APIError struct {
	Status  int
	Code    string
	Message string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("entrepot API: %d %s: %s", e.Status, e.Code, e.Message)
	}
	return fmt.Sprintf("entrepot API: HTTP %d", e.Status)
}

// IsNotFound reports whether err is a 404 API error.
func IsNotFound(err error) bool {
	var e *APIError
	return errors.As(err, &e) && e.Status == http.StatusNotFound
}

// Check converts a response into an error unless its status is one of ok.
func Check(resp *http.Response, body []byte, err error, ok ...int) error {
	if err != nil {
		return err
	}
	for _, s := range ok {
		if resp.StatusCode == s {
			return nil
		}
	}
	e := &APIError{Status: resp.StatusCode}
	var payload struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &payload) == nil {
		e.Code, e.Message = payload.Code, payload.Message
	} else if len(body) > 0 && len(body) < 512 {
		e.Message = strings.TrimSpace(string(body))
	}
	return e
}
