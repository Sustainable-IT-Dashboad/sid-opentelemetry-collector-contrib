// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dynatracereceiver

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

type Receiver struct {
	Config     *Config
	NextMetric consumer.Metrics
	ticker     *time.Ticker
	stopChan   chan struct{}
	httpClient *http.Client
	Logger     *slog.Logger
}

type DynatraceResponse struct {
	TotalCount  int                   `json:"totalCount"`
	NextPageKey string                `json:"nextPageKey"`
	Resolution  string                `json:"resolution"`
	Result      []DynatraceMetricData `json:"result"`
}

type DynatraceMetricData struct {
	MetricID string         `json:"metricId"`
	Data     []MetricValues `json:"data"`
}

type MetricValues struct {
	Timestamps   []int64           `json:"timestamps"`
	Values       []float64         `json:"values"`
	Dimensions   []string          `json:"dimensions"`
	DimensionMap map[string]string `json:"dimensionMap"`
}

// start polling from Dynatrace.
func (r *Receiver) Start(ctx context.Context, host component.Host) error { // revive:disable-line:unused-parameter
	r.Logger.Info("Dynatrace Receiver started with config:", "config", r.Config)

	r.ticker = time.NewTicker(r.Config.PollInterval)
	r.stopChan = make(chan struct{})

	// apply TLS settings to http client
	tlsConfig, err := r.Config.TLSSettings.LoadTLSConfig(ctx)
	if err != nil {
		return fmt.Errorf("failed to load TLS config: %w", err)
	}
	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}
	r.httpClient = &http.Client{
		Timeout:   r.Config.HTTPTimeout,
		Transport: transport,
	}

	go func() {
		for {
			select {
			case <-r.ticker.C:
				metrics, err := r.pullDynatraceMetrics(ctx, r.Config)
				if err != nil {
					r.Logger.Error("Error pulling metrics:", "error", err)
				}

				r.Logger.Debug("Metrics received", "metrics", metrics)
				md := convertToMetricData(metrics, r.Logger)
				r.Logger.Debug("Converted metrics", "metrics", md)
				if err := r.NextMetric.ConsumeMetrics(ctx, md); err != nil {
					r.Logger.Error("Error consuming metrics:", "error", err)
				}

			case <-r.stopChan:
				r.Logger.Info("Stopping Dynatrace Receiver polling loop.")
				return
			}
		}
	}()

	return nil
}

func (r *Receiver) Shutdown(_ context.Context) error {
	r.Logger.Info("Dynatrace Receiver shutting down.")
	r.ticker.Stop()
	close(r.stopChan)
	return nil
}

func (r *Receiver) pullDynatraceMetrics(ctx context.Context, cfg *Config) ([]DynatraceMetricData, error) {
	var metrics []DynatraceMetricData
	var err error

	for i := 0; i < cfg.MaxRetries; i++ {
		metrics, err = r.fetchAllDynatraceMetrics(ctx, cfg)
		if err == nil {
			r.Logger.Debug("Metrics recieved:", "metrics", metrics)
			return metrics, nil
		}
		r.Logger.Error("Attempt failed:", "attempt", i+1, "error", err)
		time.Sleep(time.Second * time.Duration(i+1)) // simple backoff
	}
	return nil, fmt.Errorf("all retries failed: %w", err)
}

func (r *Receiver) fetchAllDynatraceMetrics(ctx context.Context, cfg *Config) ([]DynatraceMetricData, error) {
	url := createMetricsQuery(cfg, r.Logger)

	ctx, cancel := context.WithTimeout(ctx, cfg.HTTPTimeout)
	defer cancel()

	resp, err := r.makeHttPRequest(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("request creation failed: %w", err)
	}

	body, err := readResponseBody(resp)
	if err != nil {
		return nil, err
	}

	r.Logger.Debug("Raw response from Dynatrace", "response", string(body))

	var dtResponse DynatraceResponse
	if err := json.Unmarshal(body, &dtResponse); err != nil {
		return nil, fmt.Errorf("json unmarshal failed: %w", err)
	}

	r.Logger.Debug("Parsed response from Dynatrace", "response", dtResponse)

	return dtResponse.Result, nil
}

func createMetricsQuery(cfg *Config, logger *slog.Logger) string {
	metricSelector := strings.Join(cfg.MetricSelectors, ",")
	url := fmt.Sprintf("%s?metricSelector=%s&resolution=%s&from=%s&to=%s", cfg.APIEndpoint, metricSelector, cfg.Resolution, cfg.From, cfg.To)

	logger.Debug("Fetching data from: ", "url", url)
	return url
}

func readResponseBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response body failed: %w", err)
	}
	return body, nil
}

func (r *Receiver) makeHttPRequest(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Api-Token "+r.Config.APIToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := r.httpClient.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		return nil, fmt.Errorf("dynatrace returned non-2xx status: %d", resp.StatusCode)
	}

	return resp, nil
}

func convertToMetricData(metrics []DynatraceMetricData, logger *slog.Logger) pmetric.Metrics {
	logger.Debug("------------------------ starting converter with:", "metrics", metrics)
	md := pmetric.NewMetrics()
	logger.Debug("init", "metrics", md)

	for _, metric := range metrics {
		for _, data := range metric.Data {
			rm := md.ResourceMetrics().AppendEmpty()
			sm := rm.ScopeMetrics().AppendEmpty()
			m := sm.Metrics().AppendEmpty()
			logger.Debug("rm:", "rm", rm)
			logger.Debug("sm:", "sm", sm)

			m.SetName(metric.MetricID)
			gauge := m.SetEmptyGauge()

			logger.Debug("m:", "m", m)
			for i, timestamp := range data.Timestamps {
				if i < len(data.Values) {
					dp := gauge.DataPoints().AppendEmpty()
					dp.SetTimestamp(pcommon.Timestamp(timestamp * 1e6))
					dp.SetDoubleValue(data.Values[i])

					for key, val := range data.DimensionMap {
						dp.Attributes().PutStr(key, val)
					}
					logger.Debug("dp:", "dp", dp)
				}
			}
		}
	}
	logger.Debug("------------------------ finished converter with:", "metrics", metrics)
	return md
}
