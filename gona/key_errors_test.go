package gona

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// Query encoding changes this key, as it does real ones: pages after the
// first re-encode the query, so the key reaches errors in both forms.
const exampleKey = "EXAMPLE^NOT^A^REAL^KEY"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// NetActuate takes an API key only as ?key=. In the Authorization header,
// as a Bearer token or bare, both APIs read it as a JWT and refuse it: vapi2
// with "Wrong number of segments", vapi3 with "The JWT provided in the
// authorization header is invalid." So the key can't move out of the URL.
func TestRequestsSendKeyOnlyInQuery(t *testing.T) {
	check := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if got := r.URL.Query().Get("key"); got != exampleKey {
			t.Errorf("%s: key = %q, want the API key", r.URL, got)
		}
		if got := r.Header.Get("Authorization"); got != "" {
			t.Errorf("%s: Authorization = %q, want none: NetActuate reads it as a JWT", r.URL.Path, got)
		}
		rec := httptest.NewRecorder()
		rec.WriteString(`{"result":"success","data":[]}`)
		return rec.Result(), nil
	})

	v2 := NewClientCustom(exampleKey, "https://example.invalid/")
	v2.client.Transport = check
	if err := v2.get(context.Background(), "cloud/servers", nil); err != nil {
		t.Errorf("v2: %v", err)
	}

	v3 := NewV3Client(exampleKey, "https://example.invalid")
	v3.httpClient.Transport = check
	if _, err := v3.get("oidc/clients"); err != nil {
		t.Errorf("v3: %v", err)
	}
}

// net/url and net/http put the whole URL, ?key= included, in their errors.
// Terraform prints those to terminals and CI logs, so a refused connection or
// a timeout would publish a live key.
func TestRequestErrorsDoNotExposeKey(t *testing.T) {
	refused := errors.New("connection refused")
	failing := roundTripFunc(func(*http.Request) (*http.Response, error) { return nil, refused })
	// The first page succeeds and says there is a second; fetching that fails.
	secondPageFails := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.URL.Query().Get("page") != "" {
			return nil, refused
		}
		rec := httptest.NewRecorder()
		rec.WriteString(`{"result":"success","data":{"paginator":{"current_page":1,"data":[{"id":1}],"last_page":2}}}`)
		return rec.Result(), nil
	})

	v2 := func(rt http.RoundTripper) *Client {
		c := NewClientCustom(exampleKey, "https://example.invalid/")
		c.client.Transport = rt
		return c
	}
	v3 := func(rt http.RoundTripper) *V3Client {
		c := NewV3Client(exampleKey, "https://example.invalid")
		c.httpClient.Transport = rt
		return c
	}
	ctx := context.Background()

	for _, tc := range []struct {
		name string
		call func() error
		// The request was sent, so the transport's error must stay in the
		// chain: callers tell a timeout from a refusal by it.
		sent bool
	}{
		{"v2 request", func() error { return v2(failing).get(ctx, "cloud/servers", nil) }, true},
		{"v2 second page", func() error {
			_, err := v2(secondPageFails).GetIPTransitServices(ServiceListOptions{})
			return err
		}, true},
		{"v2 contract usage", func() error { _, err := v2(failing).GetContractUsage(); return err }, true},
		{"v2 attachment", func() error {
			_, err := v2(failing).DownloadTicketAttachment("1", "ticket", "1", 0)
			return err
		}, true},
		{"v2 unparsable path", func() error { return v2(failing).get(ctx, "cloud/%zz", nil) }, false},
		{"v3 request", func() error { _, err := v3(failing).get("oidc/clients"); return err }, true},
		{"v3 unparsable path", func() error { _, err := v3(failing).get("oidc/%zz"); return err }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("got no error")
			}
			for _, form := range []string{exampleKey, url.QueryEscape(exampleKey)} {
				if strings.Contains(err.Error(), form) {
					t.Errorf("error exposes the API key as %q: %v", form, err)
				}
			}
			if tc.sent && !errors.Is(err, refused) {
				t.Errorf("error lost the transport's cause: %v", err)
			}
		})
	}
}
