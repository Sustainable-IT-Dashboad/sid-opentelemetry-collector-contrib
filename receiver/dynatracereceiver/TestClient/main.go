package main

import (
	"context"
	"log"
	"os"
	"time"

	"github.com/Sustainable-IT-Dashboad/sid-opentelemetry-collector-contrib/receiver/dynatracereceiver"
	"github.com/joho/godotenv"
	"github.com/spf13/viper"
	"go.opentelemetry.io/collector/config/configtls"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

type DummyConsumer struct{}

func (d *DummyConsumer) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	log.Printf("DummyConsumer received metrics: %d\n", md.MetricCount())
	return nil
}

func (d *DummyConsumer) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func main() {
	err := godotenv.Load()
	if err != nil {
		log.Fatal("Error loading .env file")
	}

	apiEndpoint := os.Getenv("API_ENDPOINT")
	apiToken := os.Getenv("API_TOKEN")

	if apiEndpoint == "" || apiToken == "" {
		log.Fatal("API credentials missing. Check your .env file.")
	}

	viper.SetConfigFile("../config.yaml")
	if err := viper.ReadInConfig(); err != nil {
		log.Fatal("Error reading config file:", err)
	}

	metricSelectors := viper.GetStringSlice("receivers.dynatrace.metric_selectors")
	resolution := viper.GetString("receivers.dynatrace.resolution")
	from := viper.GetString("receivers.dynatrace.from")
	to := viper.GetString("receivers.dynatrace.to")
	var tlsSettings configtls.ClientConfig
	if err := viper.UnmarshalKey("receivers.dynatrace.tls_settings", &tlsSettings); err != nil {
		// if no tls settings provided in config, leave zero value
		tlsSettings = configtls.ClientConfig{}
	}

	pollInterval := viper.GetDuration("receivers.dynatrace.poll_interval")
	if pollInterval <= 0 {
		pollInterval = 30 * time.Second
	}

	httpTimeout := viper.GetDuration("receivers.dynatrace.http_timeout")
	if httpTimeout <= 0 {
		httpTimeout = 5 * time.Second
	}

	maxRetries := viper.GetInt("receivers.dynatrace.max_retries")
	if maxRetries <= 0 {
		maxRetries = 3
	}

	if len(metricSelectors) == 0 {
		log.Fatal("No metric selectors found in config.yaml")
	}
	if resolution == "" {
		resolution = "1h"
	}

	if from == "" {
		from = "2025-04-01T00:00:00Z"
	}

	if to == "" {
		to = "2025-04-03T00:00:00Z"
	}

	config := &dynatracereceiver.Config{
		APIEndpoint:     apiEndpoint,
		APIToken:        apiToken,
		MetricSelectors: metricSelectors,
		Resolution:      resolution,
		From:            from,
		To:              to,
		PollInterval:    pollInterval,
		HTTPTimeout:     httpTimeout,
		MaxRetries:      maxRetries,
		TLSSettings:     tlsSettings, // added to test TLS settings in test client
	}

	receiver := &dynatracereceiver.Receiver{
		Config:     config,
		NextMetric: &DummyConsumer{}, //dynatraceexporter.NewSimpleExporter(),
	}

	err = receiver.Start(context.Background(), nil)
	if err != nil {
		log.Fatal("Error starting receiver:", err)
	}

	time.Sleep(2 * time.Minute)

	err = receiver.Shutdown(context.Background())
	if err != nil {
		log.Fatal("Error shutting down receiver:", err)
	}

	log.Println("Receiver stopped successfully.")
}
