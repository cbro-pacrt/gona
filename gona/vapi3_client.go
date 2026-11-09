package gona

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	V3Version      = "0.1.0"
	V3BaseEndpoint = "https://vapi3.netactuate.com"
)

type V3Client struct {
	baseURL    *url.URL
	apiKey     string
	httpClient *http.Client
	userAgent  string
	debug      bool
}

type V3APIResponse struct {
	Code int             `json:"code"`
	Data json.RawMessage `json:"data"`
}

type V3ListData struct {
	Data json.RawMessage  `json:"data"`
	Meta V3PaginationMeta `json:"meta"`
}

type V3PaginationMeta struct {
	Limit            int      `json:"limit"`
	Offset           int      `json:"offset"`
	Total            int      `json:"total"`
	SuggestedFilters []string `json:"suggestedFilters"`
}

type V3Location struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
	Flag string `json:"flag,omitempty"`
}

type V3Capacity struct {
	AutoScaling bool `json:"autoscaling"`
	RequestedGB *int `json:"requestedGB"`
	TotalGB     int  `json:"totalGB"`
}

type V3Package struct {
	ID   int    `json:"id"`
	Name string `json:"name"`
}

func NewV3Client(apiKey, baseURL string) *V3Client {
	if baseURL == "" {
		baseURL = V3BaseEndpoint
	}

	transport := &http.Transport{
		TLSNextProto: make(
			map[string]func(string, *tls.Conn) http.RoundTripper,
		),
	}

	endpoint, _ := url.Parse(baseURL)

	return &V3Client{
		baseURL:   endpoint,
		apiKey:    apiKey,
		userAgent: "gona-v3/" + V3Version,
		debug:     os.Getenv("NA_API_DEBUG") != "",
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   120 * time.Second,
		},
	}
}

func (c *V3Client) debugLog(format string, v ...interface{}) {
	if !c.debug {
		return
	}
	log.Printf("[DEBUG][V3] "+format, v...)
}

func (c *V3Client) v3ApiKeyPath(path string) string {
	if strings.Contains(path, "?") {
		return path + "&key=" + c.apiKey
	}
	return path + "?key=" + c.apiKey
}

func (c *V3Client) doRequest(method, path string, body interface{}) (*V3APIResponse, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonData, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		c.debugLog("%s request body for %s: %s", method, path, string(jsonData))
		bodyReader = bytes.NewBuffer(jsonData)
	}

	relPath, err := url.Parse(c.v3ApiKeyPath(path))
	if err != nil {
		return nil, fmt.Errorf("failed to parse path %q: %w", path, withoutKey(err, c.apiKey))
	}

	fullURL := c.baseURL.ResolveReference(relPath)

	req, err := http.NewRequest(method, fullURL.String(), bodyReader)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", withoutKey(err, c.apiKey))
	}

	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	c.debugLog("%s %s", method, redactV3URL(fullURL))

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", withoutKey(err, c.apiKey))
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	c.debugLog("response status=%d body=%s", resp.StatusCode, string(respBody))

	if resp.StatusCode == 204 {
		return &V3APIResponse{Code: 204}, nil
	}

	if isSemanticNotFound(resp.StatusCode, string(respBody)) {
		return nil, &V3NotFoundError{
			StatusCode: resp.StatusCode,
			Body:       string(respBody),
		}
	}

	var apiResp V3APIResponse
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response (status %d): %s", resp.StatusCode, string(respBody))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, respBody, "", "  "); err == nil {
			return &apiResp, fmt.Errorf("API error on %s %s: HTTP %d\n%s",
				method, path, resp.StatusCode, pretty.String())
		}
		return &apiResp, fmt.Errorf("API error on %s %s: HTTP %d\n%s",
			method, path, resp.StatusCode, string(respBody))
	}

	if apiResp.Code != 0 && (apiResp.Code < 200 || apiResp.Code >= 300) {
		var pretty bytes.Buffer
		if err := json.Indent(&pretty, respBody, "", "  "); err == nil {
			return &apiResp, fmt.Errorf("API error on %s %s: code %d\n%s",
				method, path, apiResp.Code, pretty.String())
		}
		return &apiResp, fmt.Errorf("API error on %s %s: code %d\n%s",
			method, path, apiResp.Code, string(respBody))
	}

	return &apiResp, nil
}

func (c *V3Client) get(path string) (*V3APIResponse, error) {
	return c.doRequest("GET", path, nil)
}

// unwrapV3List reads the {meta,data} pagination envelope out of a vAPI3 response body.
//
// Most families put it directly at data.{meta,data}. Some nest it under a named key instead:
// OIDC auth and change logs arrive as data.logs.{meta,data} and client keys as
// data.keys.{meta,data}. Passing the empty key selects the flat shape. Getting this wrong is
// silent rather than loud: the envelope unmarshals fine, the inner Data is nil, and the failure
// surfaces later as "unexpected end of JSON input".
func unwrapV3List(raw json.RawMessage, key string) (V3ListData, error) {
	if key != "" {
		var nested map[string]json.RawMessage
		if err := json.Unmarshal(raw, &nested); err != nil {
			return V3ListData{}, err
		}
		inner, ok := nested[key]
		if !ok {
			return V3ListData{}, fmt.Errorf("vAPI3 list response has no %q key", key)
		}
		raw = inner
	}

	return parseV3List(raw)
}

func parseV3List(raw json.RawMessage) (V3ListData, error) {
	if len(raw) == 0 {
		return V3ListData{}, fmt.Errorf("vAPI3 list response has empty data")
	}
	if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) {
		return V3ListData{Data: raw}, nil
	}

	var listData V3ListData
	if err := json.Unmarshal(raw, &listData); err != nil {
		return V3ListData{}, err
	}
	if len(listData.Data) > 0 {
		trimmedData := bytes.TrimSpace(listData.Data)
		if bytes.HasPrefix(trimmedData, []byte("[")) {
			return listData, nil
		}
		if bytes.HasPrefix(trimmedData, []byte("{")) && hasV3ListDataKey(listData.Data) {
			innerList, err := parseV3List(listData.Data)
			if err == nil && len(innerList.Data) > 0 {
				return innerList, nil
			}
		}
		return listData, nil
	}

	var nested map[string]json.RawMessage
	if err := json.Unmarshal(raw, &nested); err != nil {
		return V3ListData{}, err
	}
	for nestedKey, inner := range nested {
		if nestedKey == "meta" {
			continue
		}
		if !hasV3ListDataKey(inner) {
			continue
		}
		innerList, err := parseV3List(inner)
		if err == nil && len(innerList.Data) > 0 {
			return innerList, nil
		}
	}
	return V3ListData{}, fmt.Errorf("vAPI3 list response has no list data")
}

func hasV3ListDataKey(raw json.RawMessage) bool {
	if !bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
		return false
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return false
	}
	_, ok := object["data"]
	return ok
}

func (c *V3Client) getList(path string) (*V3ListData, error) {
	return c.getListUnder(path, "")
}

func (c *V3Client) getListUnder(path, key string) (*V3ListData, error) {
	resp, err := c.get(path)
	if err != nil {
		return nil, err
	}

	listData, err := unwrapV3List(resp.Data, key)
	if err != nil {
		return nil, err
	}

	var allData []json.RawMessage
	if err := json.Unmarshal(listData.Data, &allData); err != nil {
		return nil, err
	}

	meta := listData.Meta
	for meta.Total > 0 && meta.Limit > 0 && meta.Offset+meta.Limit < meta.Total {
		nextOffset := meta.Offset + meta.Limit
		nextPath, err := v3ListPagePath(path, nextOffset, meta.Limit)
		if err != nil {
			return nil, err
		}

		resp, err := c.get(nextPath)
		if err != nil {
			return nil, err
		}

		nextListData, err := unwrapV3List(resp.Data, key)
		if err != nil {
			return nil, err
		}

		var pageData []json.RawMessage
		if err := json.Unmarshal(nextListData.Data, &pageData); err != nil {
			return nil, err
		}

		allData = append(allData, pageData...)

		// Guard against a page that does not advance. vAPI3 honours offset and echoes it
		// back, so this should never fire, but a list call
		// that loops forever against the API is a worse bug than the truncation this
		// helper exists to fix. Fail closed with what we have rather than spin.
		if nextListData.Meta.Offset <= meta.Offset || len(pageData) == 0 {
			break
		}
		meta = nextListData.Meta
	}

	data, err := json.Marshal(allData)
	if err != nil {
		return nil, err
	}
	listData.Data = data
	return &listData, nil
}

func v3ListPagePath(path string, offset, limit int) (string, error) {
	u, err := url.Parse(path)
	if err != nil {
		return "", err
	}

	query := u.Query()
	query.Set("offset", strconv.Itoa(offset))
	query.Set("limit", strconv.Itoa(limit))
	u.RawQuery = query.Encode()
	return u.String(), nil
}

func (c *V3Client) post(path string, body interface{}) (*V3APIResponse, error) {
	return c.doRequest("POST", path, body)
}

func (c *V3Client) patch(path string, body interface{}) (*V3APIResponse, error) {
	return c.doRequest("PATCH", path, body)
}

func (c *V3Client) put(path string, body interface{}) (*V3APIResponse, error) {
	return c.doRequest("PUT", path, body)
}

func (c *V3Client) del(path string) (*V3APIResponse, error) {
	return c.doRequest("DELETE", path, nil)
}

type V3NotFoundError struct {
	StatusCode int
	Body       string
}

func (e *V3NotFoundError) Error() string {
	return fmt.Sprintf("resource not found (HTTP %d): %s", e.StatusCode, e.Body)
}

// redactV3URL strips the API key from a V3 URL before it reaches the debug log.
// Same reasoning as redactURL in client.go: the key travels as a query parameter.
func redactV3URL(u *url.URL) string {
	if u == nil {
		return ""
	}
	c := *u
	q := c.Query()
	if q.Get("key") != "" {
		q.Set("key", "REDACTED")
		c.RawQuery = q.Encode()
	}
	return c.String()
}

func IsV3NotFound(err error) bool {
	if err == nil {
		return false
	}
	var notFound *V3NotFoundError
	return errors.As(err, &notFound)
}

// isTransientServerError returns true for 5xx errors that are likely transient
// (e.g. the gateway VM not yet ready immediately after VPC creation).
func isTransientServerError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "HTTP 500") ||
		strings.Contains(msg, "HTTP 502") ||
		strings.Contains(msg, "HTTP 503") ||
		strings.Contains(msg, "HTTP 504")
}

// isVPCNotReadyError returns true when the API reports a 400 because a VPC
// exists but child services are still initializing.
func isVPCNotReadyError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "http 400") &&
		strings.Contains(msg, "vpc") &&
		strings.Contains(msg, "not ready")
}

// postWithRetry calls POST on path and retries up to maxRetries times with
// retryInterval between attempts when a transient server error is returned.
// Use this for apply-changes endpoints that may return 5xx briefly after VPC
// creation while the gateway VM is still initialising.
func (c *V3Client) postWithRetry(path string, maxRetries int, retryInterval time.Duration) (*V3APIResponse, error) {
	resp, err := c.post(path, nil)
	if err == nil {
		return resp, nil
	}
	for i := 0; i < maxRetries && isTransientServerError(err); i++ {
		c.debugLog("POST %s returned transient error (attempt %d/%d): %v, retrying in %v",
			path, i+1, maxRetries, err, retryInterval)
		time.Sleep(retryInterval)
		resp, err = c.post(path, nil)
		if err == nil {
			return resp, nil
		}
	}
	return nil, err
}

func isSemanticNotFound(statusCode int, body string) bool {
	if statusCode == 404 || statusCode == 410 {
		return true
	}

	lower := strings.ToLower(body)
	notFoundMessages := []string{
		"does not exist",
		"not associated with your account",
		"not found",
	}

	for _, msg := range notFoundMessages {
		if strings.Contains(lower, msg) {
			return true
		}
	}

	return false
}
