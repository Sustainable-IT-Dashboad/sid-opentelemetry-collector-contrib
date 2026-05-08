// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dynatracereceiver

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"log/slog"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/collector/config/configtls"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

func TestReceiver_StartAndShutdown(t *testing.T) {
	cfg := &Config{
		APIEndpoint:     "http://example.com",
		APIToken:        "token",
		MetricSelectors: []string{"builtin:metric"},
		PollInterval:    10 * time.Millisecond,
		HTTPTimeout:     1 * time.Second,
		MaxRetries:      1,
	}
	dummyConsumer := &DummyConsumer{}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	receiver := &Receiver{
		Config:     cfg,
		NextMetric: dummyConsumer,
		httpClient: &http.Client{},
		Logger:     logger,
	}

	ctx := context.Background()
	err := receiver.Start(ctx, nil)
	assert.NoError(t, err, "Receiver should start without error")
	time.Sleep(50 * time.Millisecond)
	err = receiver.Shutdown(ctx)
	assert.NoError(t, err, "Receiver should shutdown")
}

type DummyConsumer struct{}

func (d *DummyConsumer) ConsumeMetrics(_ context.Context, _ pmetric.Metrics) error {
	return nil
}

func (d *DummyConsumer) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func TestPullDynatraceMetrics_Retry(t *testing.T) {
	var requestCount int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		if requestCount < 3 {
			http.Error(w, "failure", http.StatusInternalServerError)
			return
		}

		fmt.Fprintln(w, `{
			"totalCount": 1,
			"result": [{"metricId": "test.metric", "data": []}]
		}`)
	}))
	defer server.Close()
	cfg := &Config{
		APIEndpoint: server.URL,
		APIToken:    "dummy-token",
		MaxRetries:  5,
		HTTPTimeout: 2 * time.Second,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	receiver := &Receiver{
		Config:     cfg,
		httpClient: server.Client(),
		Logger:     logger,
	}

	ctx := context.Background()
	metrics, err := receiver.pullDynatraceMetrics(ctx, cfg)
	assert.NoError(t, err)
	assert.Len(t, metrics, 1)
	assert.Equal(t, 3, requestCount, "Should retry twice before succeeding")
}

func TestFetchAllDynatraceMetrics(t *testing.T) {
	mockResponse := `{
		"totalCount": 1,
		"nextPageKey": null,
		"resolution": "1h",
		"result": [{
			"metricId": "builtin:containers.cpu.usageTime",
			"data": [{
				"timestamps": [1712203200000],
				"values": [55.0],
				"dimensions": ["container-xyz"],
				"dimensionMap": {
					"container": "testapp"
				}
			}]
		}]
	}`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.Header.Get("Authorization"), "Api-Token")
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, mockResponse)
	}))
	defer server.Close()
	cfg := &Config{
		APIEndpoint:     server.URL,
		APIToken:        "dummy-token",
		MetricSelectors: []string{"builtin:containers.cpu.usageTime"},
		Resolution:      "1h",
		From:            "2025-04-01T00:00:00Z",
		To:              "2025-04-02T00:00:00Z",
		MaxRetries:      1,
		HTTPTimeout:     2 * time.Second,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	receiver := &Receiver{
		Config:     cfg,
		httpClient: server.Client(),
		Logger:     logger,
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPTimeout)
	defer cancel()
	result, err := receiver.fetchAllDynatraceMetrics(ctx, cfg)

	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "builtin:containers.cpu.usageTime", result[0].MetricID)
	assert.Equal(t, 1, len(result[0].Data))
	assert.Equal(t, 55.0, result[0].Data[0].Values[0])
	assert.Equal(t, "testapp", result[0].Data[0].DimensionMap["container"])
}

func TestFetchAllDynatraceMetrics_InvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintln(w, "<html>Not JSON</html>")
	}))
	defer server.Close()
	cfg := &Config{
		APIEndpoint:     server.URL,
		APIToken:        "dummy-token",
		MetricSelectors: []string{"builtin:containers.cpu.usageTime"},
		Resolution:      "1h",
		From:            "2025-04-01T00:00:00Z",
		To:              "2025-04-02T00:00:00Z",
		HTTPTimeout:     2 * time.Second,
		MaxRetries:      1,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	receiver := &Receiver{
		Config:     cfg,
		httpClient: server.Client(),
		Logger:     logger,
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.HTTPTimeout)
	defer cancel()
	_, err := receiver.fetchAllDynatraceMetrics(ctx, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "json unmarshal failed")
}

func TestFetchAllDynatraceMetrics_HttpError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer server.Close()
	cfg := &Config{
		APIEndpoint:     server.URL,
		APIToken:        "dummy-token",
		MetricSelectors: []string{"builtin:containers.cpu.usageTime"},
		Resolution:      "1h",
		From:            "2025-04-01T00:00:00Z",
		To:              "2025-04-02T00:00:00Z",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	receiver := &Receiver{
		Config:     cfg,
		httpClient: server.Client(),
		Logger:     logger,
	}
	_, err := receiver.fetchAllDynatraceMetrics(context.Background(), cfg)
	assert.Error(t, err)
}

func TestFetchAllDynatraceMetrics_HTTPTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(200 * time.Millisecond) // Delay to trigger timeout
		fmt.Fprintln(w, "{}")
	}))
	defer server.Close()
	cfg := &Config{
		APIEndpoint: server.URL,
		APIToken:    "dummy-token",
		HTTPTimeout: 50 * time.Millisecond, // Short timeout
		MaxRetries:  1,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	receiver := &Receiver{
		Config:     cfg,
		httpClient: server.Client(),
		Logger:     logger,
	}

	ctx := context.Background()
	_, err := receiver.fetchAllDynatraceMetrics(ctx, cfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "context deadline exceeded")
}

func TestConvertToMetricData(t *testing.T) {
	sample := []DynatraceMetricData{
		{
			MetricID: "builtin:containers.cpu.usageTime",
			Data: []MetricValues{
				{
					Timestamps: []int64{1712203200000},
					Values:     []float64{42.0},
					DimensionMap: map[string]string{
						"container": "example-container",
					},
				},
			},
		},
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	result := convertToMetricData(sample, logger)

	metrics := result.ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics()
	assert.Equal(t, 1, metrics.Len(), "Expected one metric")

	metric := metrics.At(0)
	assert.Equal(t, "builtin:containers.cpu.usageTime", metric.Name())
	assert.Equal(t, pmetric.MetricTypeGauge, metric.Type())
	assert.Equal(t, 1, metric.Gauge().DataPoints().Len())
	assert.Equal(t, 42.0, metric.Gauge().DataPoints().At(0).DoubleValue())

	val, exists := metric.Gauge().DataPoints().At(0).Attributes().Get("container")
	assert.True(t, exists, "container attribute should exist")
	assert.Equal(t, "example-container", val.Str())
}

func TestDynatraceURLGenerationFromConfig(t *testing.T) {

	_ = os.Setenv("API_ENDPOINT", "https://dummy.dynatrace.com/api/v2/metrics/query")
	_ = os.Setenv("API_TOKEN", "dummy-token")
	_ = os.Setenv("DEPLOYMENT_ENVIRONMENT", "prod")
	_ = os.Setenv("PROJECT_NAME", "my-customer-project")
	_ = os.Setenv("DEPLOYMENT_REGION", "eu-west-1")

	viper.SetConfigFile("config.yaml")
	err := viper.ReadInConfig()
	assert.NoError(t, err, "Config file should be read successfully")

	// Unmarshal into your config struct
	var cfg Config
	err = viper.UnmarshalKey("receivers.dynatrace", &cfg)
	assert.NoError(t, err, "Config should unmarshal correctly")

	assert.Equal(t, "now", cfg.To)
	assert.Equal(t, "now-1m", cfg.From)
	assert.Equal(t, "1m", cfg.Resolution)
	assert.NotEmpty(t, cfg.APIEndpoint)

	metricSelector := "builtin:containers.cpu.usageTime"
	url := fmt.Sprintf("%s?metricSelector=%s&resolution=%s&from=%s&to=%s",
		cfg.APIEndpoint,
		metricSelector,
		cfg.Resolution,
		cfg.From,
		cfg.To,
	)

	slog.Info("Generated Dynatrace Query URL:", "url", url)

}

func genSelfSignedCert() (certPEM, keyPEM []byte, err error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return nil, nil, err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, err
	}

	tmpl := x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			Organization: []string{"test-local"},
		},
		NotBefore:   time.Now().Add(-time.Hour),
		NotAfter:    time.Now().Add(24 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:    []string{"localhost"},
	}

	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, nil, err
	}

	certBuf := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyBuf := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return certBuf, keyBuf, nil
}

func TestTLSInsecureSkipVerify(t *testing.T) {
	certPEM, keyPEM, err := genSelfSignedCert()
	if err != nil {
		t.Fatalf("failed to generate cert: %v", err)
	}

	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("failed to parse keypair: %v", err)
	}

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"totalCount":1,"result":[{"metricId":"test.metric","data":[]}]} `))
	})

	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	server.StartTLS()
	defer server.Close()

	tests := []struct {
		name     string
		insecure bool
		wantErr  bool
	}{
		{"verify_off", true, false},
		{"verify_on", false, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &Config{
				APIEndpoint:     server.URL,
				APIToken:        "dummy",
				MetricSelectors: []string{"metric"},
				HTTPTimeout:     2 * time.Second,
				MaxRetries:      1,
			}

			// set TLS setting per test
			cfg.TLSSettings = configtls.ClientConfig{InsecureSkipVerify: tc.insecure}

			logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
			r := &Receiver{Config: cfg, Logger: logger}

			// build httpClient like receiver.Start does
			tlsCfg, err := cfg.TLSSettings.LoadTLSConfig(context.Background())
			if err != nil {
				t.Fatalf("LoadTLSConfig failed: %v", err)
			}
			transport := &http.Transport{TLSClientConfig: tlsCfg}
			r.httpClient = &http.Client{Timeout: cfg.HTTPTimeout, Transport: transport}

			ctx := context.Background()
			_, err = r.fetchAllDynatraceMetrics(ctx, cfg)
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected error but got none")
				}
			} else {
				if err != nil {
					t.Fatalf("expected success but got error: %v", err)
				}
			}
		})
	}
}

func TestCreateMetricsQuery_WithHostIDs(t *testing.T) {
	cfg := &Config{
		APIEndpoint:     "https://dummy.dynatrace.com/api/v2/metrics/query",
		MetricSelectors: []string{"builtin:host.cpu.usage", "builtin:host.mem.used"},
		HostIDs:         []string{"HOST-xyz", "HOST-abc"},
		Resolution:      "1m",
		From:            "now-1m",
		To:              "now",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	queryURL := createMetricsQuery(cfg, logger)

	parsedURL, err := url.Parse(queryURL)
	assert.NoError(t, err)

	params := parsedURL.Query()

	assert.Equal(t, "builtin:host.cpu.usage,builtin:host.mem.used", params.Get("metricSelector"))
	assert.Equal(t, "1m", params.Get("resolution"))
	assert.Equal(t, "now-1m", params.Get("from"))
	assert.Equal(t, "now", params.Get("to"))
	assert.Equal(t, `type("HOST"),entityId("HOST-xyz","HOST-abc")`, params.Get("entitySelector"))
}

func TestCreateMetricsQuery_WithoutHostIDs(t *testing.T) {
	cfg := &Config{
		APIEndpoint:     "https://dummy.dynatrace.com/api/v2/metrics/query",
		MetricSelectors: []string{"builtin:host.cpu.usage"},
		HostIDs:         []string{},
		Resolution:      "1m",
		From:            "now-1m",
		To:              "now",
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	queryURL := createMetricsQuery(cfg, logger)

	parsedURL, err := url.Parse(queryURL)
	assert.NoError(t, err)

	params := parsedURL.Query()

	assert.Equal(t, "builtin:host.cpu.usage", params.Get("metricSelector"))
	assert.Equal(t, "", params.Get("entitySelector"))
}

func TestFetchAllDynatraceMetrics_EnrichesHostNameAndCachesLookup(t *testing.T) {
	metricsResponse := `{
        "totalCount": 1,
        "resolution": "1m",
        "result": [{
            "metricId": "builtin:host.cpu.usage",
            "data": [{
                "timestamps": [1712203200000],
                "values": [55.0],
                "dimensions": ["HOST-xyz"],
                "dimensionMap": {
                    "dt.entity.host": "HOST-xyz"
                }
            }]
        }]
    }`

	entitiesResponse := `{
        "totalCount": 1,
        "pageSize": 1,
        "entities": [{
            "entityId": "HOST-xyz",
            "displayName": "my-readable-host-name"
        }]
    }`

	var entitiesRequestCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.Header.Get("Authorization"), "Api-Token")
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v2/metrics/query":
			fmt.Fprintln(w, metricsResponse)

		case "/api/v2/entities":
			entitiesRequestCount++

			assert.Equal(t, `type("HOST"),entityId("HOST-xyz")`, r.URL.Query().Get("entitySelector"))

			fmt.Fprintln(w, entitiesResponse)

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := &Config{
		APIEndpoint:     server.URL + "/api/v2/metrics/query",
		APIToken:        "dummy-token",
		MetricSelectors: []string{"builtin:host.cpu.usage"},
		HostIDs:         []string{"HOST-xyz"},
		Resolution:      "1m",
		From:            "now-1m",
		To:              "now",
		MaxRetries:      1,
		HTTPTimeout:     2 * time.Second,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	receiver := &Receiver{
		Config:     cfg,
		httpClient: server.Client(),
		Logger:     logger,
	}

	ctx := context.Background()

	firstResult, err := receiver.fetchAllDynatraceMetrics(ctx, cfg)
	assert.NoError(t, err)
	assert.Len(t, firstResult, 1)

	firstDimensionMap := firstResult[0].Data[0].DimensionMap
	assert.Equal(t, "HOST-xyz", firstDimensionMap["dt.entity.host"])
	assert.Equal(t, "my-readable-host-name", firstDimensionMap["host.name"])

	secondResult, err := receiver.fetchAllDynatraceMetrics(ctx, cfg)
	assert.NoError(t, err)
	assert.Len(t, secondResult, 1)

	secondDimensionMap := secondResult[0].Data[0].DimensionMap
	assert.Equal(t, "HOST-xyz", secondDimensionMap["dt.entity.host"])
	assert.Equal(t, "my-readable-host-name", secondDimensionMap["host.name"])

	assert.Equal(t, 1, entitiesRequestCount, "host name should be resolved only once due to cache")
}

func TestFetchAllDynatraceMetrics_LeavesHostNameEmptyWhenEntityNameMissing(t *testing.T) {
	metricsResponse := `{
        "totalCount": 1,
        "resolution": "1m",
        "result": [{
            "metricId": "builtin:host.cpu.usage",
            "data": [{
                "timestamps": [1712203200000],
                "values": [55.0],
                "dimensions": ["HOST-xyz"],
                "dimensionMap": {
                    "dt.entity.host": "HOST-xyz"
                }
            }]
        }]
    }`

	entitiesResponse := `{
        "totalCount": 1,
        "pageSize": 1,
        "entities": [{
            "entityId": "HOST-xyz",
            "displayName": ""
        }]
    }`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.Header.Get("Authorization"), "Api-Token")
		w.Header().Set("Content-Type", "application/json")

		switch r.URL.Path {
		case "/api/v2/metrics/query":
			fmt.Fprintln(w, metricsResponse)

		case "/api/v2/entities":
			assert.Equal(t, `type("HOST"),entityId("HOST-xyz")`, r.URL.Query().Get("entitySelector"))
			fmt.Fprintln(w, entitiesResponse)

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := &Config{
		APIEndpoint:     server.URL + "/api/v2/metrics/query",
		APIToken:        "dummy-token",
		MetricSelectors: []string{"builtin:host.cpu.usage"},
		HostIDs:         []string{"HOST-xyz"},
		Resolution:      "1m",
		From:            "now-1m",
		To:              "now",
		MaxRetries:      1,
		HTTPTimeout:     2 * time.Second,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	receiver := &Receiver{
		Config:     cfg,
		httpClient: server.Client(),
		Logger:     logger,
	}

	result, err := receiver.fetchAllDynatraceMetrics(context.Background(), cfg)

	assert.NoError(t, err)
	assert.Len(t, result, 1)

	dimensionMap := result[0].Data[0].DimensionMap

	assert.Equal(t, "HOST-xyz", dimensionMap["dt.entity.host"])
	assert.Empty(t, dimensionMap["host.name"])
}

func TestFetchAllDynatraceMetrics_ContinuesWhenEntitiesAPIFails(t *testing.T) {
	metricsResponse := `{
        "totalCount": 1,
        "resolution": "1m",
        "result": [{
            "metricId": "builtin:host.cpu.usage",
            "data": [{
                "timestamps": [1712203200000],
                "values": [55.0],
                "dimensions": ["HOST-xyz"],
                "dimensionMap": {
                    "dt.entity.host": "HOST-xyz"
                }
            }]
        }]
    }`

	var entitiesRequestCount int

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.Header.Get("Authorization"), "Api-Token")

		switch r.URL.Path {
		case "/api/v2/metrics/query":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprintln(w, metricsResponse)

		case "/api/v2/entities":
			entitiesRequestCount++
			http.Error(w, "forbidden", http.StatusForbidden)

		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	cfg := &Config{
		APIEndpoint:     server.URL + "/api/v2/metrics/query",
		APIToken:        "dummy-token",
		MetricSelectors: []string{"builtin:host.cpu.usage"},
		HostIDs:         []string{"HOST-xyz"},
		Resolution:      "1m",
		From:            "now-1m",
		To:              "now",
		MaxRetries:      1,
		HTTPTimeout:     2 * time.Second,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	receiver := &Receiver{
		Config:     cfg,
		httpClient: server.Client(),
		Logger:     logger,
	}

	result, err := receiver.fetchAllDynatraceMetrics(context.Background(), cfg)

	assert.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, 1, entitiesRequestCount)

	dimensionMap := result[0].Data[0].DimensionMap

	assert.Equal(t, "HOST-xyz", dimensionMap["dt.entity.host"])
	assert.Empty(t, dimensionMap["host.name"])
}

func TestReceiver_DoesNotConsumeMetricsWhenPullFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer server.Close()

	consumed := make(chan struct{}, 1)

	cfg := &Config{
		APIEndpoint:     server.URL,
		APIToken:        "dummy-token",
		MetricSelectors: []string{"builtin:host.cpu.usage"},
		Resolution:      "1m",
		From:            "now-1m",
		To:              "now",
		PollInterval:    10 * time.Millisecond,
		HTTPTimeout:     200 * time.Millisecond,
		MaxRetries:      1,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))

	receiver := &Receiver{
		Config: cfg,
		NextMetric: &FailOnConsumeConsumer{
			consumed: consumed,
		},
		Logger: logger,
	}

	ctx := context.Background()

	err := receiver.Start(ctx, nil)
	assert.NoError(t, err)

	defer func() {
		err := receiver.Shutdown(ctx)
		assert.NoError(t, err)
	}()

	select {
	case <-consumed:
		t.Fatal("ConsumeMetrics should not be called when pulling Dynatrace metrics fails")
	case <-time.After(100 * time.Millisecond):
		// Expected: no metrics should be consumed when the pull fails.
	}
}

type FailOnConsumeConsumer struct {
	consumed chan struct{}
}

func (c *FailOnConsumeConsumer) ConsumeMetrics(_ context.Context, _ pmetric.Metrics) error {
	select {
	case c.consumed <- struct{}{}:
	default:
	}
	return nil
}

func (c *FailOnConsumeConsumer) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}
