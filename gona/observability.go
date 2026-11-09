package gona

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
)

type ContractUsage struct {
	ID                  *int    `json:"id"`
	ContractMBPkgID     *int    `json:"contract_mbpkgid"`
	ParentContractID    *int    `json:"parent_contract_id"`
	Brand               *string `json:"brand"`
	MBID                *int    `json:"mb_id"`
	ContractType        *string `json:"contract_type"`
	IsFree              *int    `json:"is_free"`
	IncludeBandwidth    *int    `json:"include_bandwidth"`
	CustomerPO          *string `json:"customer_po"`
	CustomerDescription *string `json:"customer_description"`
	POMonthlyLimit      *int    `json:"po_monthly_limit"`
	MonthlyDiscount     *int    `json:"monthly_discount"`
	HourlyDiscount      *int    `json:"hourly_discount"`
	MaxCPUs             *int    `json:"max_cpus"`
	MaxRAM              *int    `json:"max_ram"`
	MaxDisk             *int    `json:"max_disk"`
	AllowOverage        *int    `json:"allow_overage"`
}

func (c *Client) GetContractUsage() (ContractUsage, error) {
	req, err := c.newRequest(context.Background(), "GET", "cloud/contract/usage", nil)
	if err != nil {
		return ContractUsage{}, fmt.Errorf("get contract usage: %w", err)
	}

	resp, err := c.send(req)
	if err != nil {
		return ContractUsage{}, fmt.Errorf("get contract usage: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ContractUsage{}, fmt.Errorf("get contract usage: %w", err)
	}

	var envelope apiResponse
	if err := json.Unmarshal(body, &envelope); err != nil {
		return ContractUsage{}, fmt.Errorf("get contract usage: could not unmarshal response: %w", err)
	}

	if resp.StatusCode == http.StatusNotFound {
		return ContractUsage{}, &NotFoundError{
			Method:     req.Method,
			URL:        redactURL(req.URL),
			StatusCode: resp.StatusCode,
			Code:       envelope.Code,
			Message:    envelope.Message,
			Body:       string(envelope.Data),
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 || envelope.Result != "success" {
		return ContractUsage{}, fmt.Errorf("get contract usage: got an error response on %s %s: code %d / %d, response: %s / %s",
			req.Method, redactURL(req.URL), resp.StatusCode, envelope.Code, envelope.Message, string(envelope.Data))
	}

	var usage ContractUsage
	if err := json.Unmarshal(envelope.Data, &usage); err != nil {
		return ContractUsage{}, fmt.Errorf("get contract usage: could not unmarshal response data (%d bytes, redacted): %w",
			len(envelope.Data), err)
	}

	return usage, nil
}

type StatisticSample struct {
	Count     float64 `json:"count"`
	Resources float64 `json:"resources"`
	Avg       float64 `json:"avg"`
	Sum       float64 `json:"sum"`
}

type StatisticResult struct {
	Metric  string
	Service string            `json:"service"`
	Data    []StatisticSample `json:"data"`
}

type statisticsResultWire struct {
	Metric  map[string]json.RawMessage `json:"metric"`
	Service string                     `json:"service"`
	Data    []StatisticSample          `json:"data"`
}

func (r *StatisticResult) UnmarshalJSON(data []byte) error {
	var wire statisticsResultWire
	if err := json.Unmarshal(data, &wire); err != nil {
		return err
	}

	keys := make([]string, 0, len(wire.Metric))
	for metric := range wire.Metric {
		keys = append(keys, metric)
	}
	sort.Strings(keys)

	if len(keys) > 0 {
		r.Metric = keys[0]
	}
	r.Service = wire.Service
	r.Data = wire.Data
	return nil
}

type statisticMetricRequest struct {
	Metric map[string]map[string]interface{} `json:"metric"`
}

type statisticsRequest struct {
	Metrics []statisticMetricRequest `json:"metrics"`
}

func buildStatisticsRequest(metrics []string) statisticsRequest {
	req := statisticsRequest{Metrics: make([]statisticMetricRequest, len(metrics))}
	for i, metric := range metrics {
		req.Metrics[i] = statisticMetricRequest{
			Metric: map[string]map[string]interface{}{
				metric: {},
			},
		}
	}
	return req
}

func (c *V3Client) QueryStatistics(metrics []string) ([]StatisticResult, error) {
	return c.queryStatistics("/cloud/statistics", metrics)
}

func (c *V3Client) QueryNetworkingStatistics(metrics []string) ([]StatisticResult, error) {
	return c.queryStatistics("/cloud/networking/statistics", metrics)
}

func (c *V3Client) QueryAnycastStatistics(metrics []string) ([]StatisticResult, error) {
	return c.queryStatistics("/cloud/networking/anycast/statistics", metrics)
}

func (c *V3Client) queryStatistics(path string, metrics []string) ([]StatisticResult, error) {
	resp, err := c.post(path, buildStatisticsRequest(metrics))
	if err != nil {
		return nil, fmt.Errorf("query statistics %s: %w", path, err)
	}

	var results []StatisticResult
	if err := json.Unmarshal(resp.Data, &results); err != nil {
		return nil, fmt.Errorf("query statistics %s: %w", path, err)
	}
	return results, nil
}

type MetricNames struct {
	TimeWindow MetricTimeWindow
	Metrics    []MetricName
}

type MetricTimeWindow struct {
	Start   string `json:"start"`
	End     string `json:"end"`
	Seconds int    `json:"seconds"`
}

type MetricName struct {
	Metric    string
	Service   string        `json:"service"`
	Resources int           `json:"resources"`
	Avg       MetricSummary `json:"avg"`
	Last      MetricSummary `json:"last"`
	Sum       MetricSummary `json:"sum"`
}

type MetricSummary struct {
	Sum float64 `json:"sum"`
	Avg float64 `json:"avg"`
	Min float64 `json:"min"`
	Max float64 `json:"max"`
}

func (c *V3Client) GetMetricNames() (MetricNames, error) {
	resp, err := c.post("/cloud/statistics/views/all-metrics", map[string]interface{}{})
	if err != nil {
		return MetricNames{}, fmt.Errorf("get metric names: %w", err)
	}

	var raw map[string]json.RawMessage
	if err := json.Unmarshal(resp.Data, &raw); err != nil {
		return MetricNames{}, fmt.Errorf("get metric names: %w", err)
	}

	result := MetricNames{}
	if rawWindow, ok := raw["__timeWindow"]; ok {
		if err := json.Unmarshal(rawWindow, &result.TimeWindow); err != nil {
			return MetricNames{}, fmt.Errorf("get metric names time window: %w", err)
		}
	}

	names := make([]string, 0, len(raw))
	for name := range raw {
		if strings.HasPrefix(name, "__") {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)

	result.Metrics = make([]MetricName, len(names))
	for i, name := range names {
		var metric MetricName
		if err := json.Unmarshal(raw[name], &metric); err != nil {
			return MetricNames{}, fmt.Errorf("get metric names %s: %w", name, err)
		}
		metric.Metric = name
		result.Metrics[i] = metric
	}

	return result, nil
}
