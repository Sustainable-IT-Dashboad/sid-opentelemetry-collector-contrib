// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dynatracereceiver

import (
	"context"
	"time"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/receiver"
	"go.opentelemetry.io/collector/config/configtls"
)

const TypeStr = "dynatrace"

func NewFactory() receiver.Factory {
	return receiver.NewFactory(
		component.MustNewType(TypeStr),
		createDefaultConfig,
		receiver.WithMetrics(createMetricsReceiver, component.StabilityLevelDevelopment),
	)
}

func createDefaultConfig() component.Config {
	return &Config{
		APIEndpoint:     "https://YourEndpoint.live.dynatrace.com/api/v2/metrics/query", // Placeholder
		APIToken:        "",
		MetricSelectors: []string{},
		Resolution:      "1m",
		From:            "now-1m",
		To:              "now",
		PollInterval:    30 * time.Second,
		MaxRetries:      3,
		HTTPTimeout:     5 * time.Second,
		TLSSettings:     configtls.ClientConfig{InsecureSkipVerify: false}, // By default, do not skip TLS verification. Users can override this in their config to handle self-signed certificates.
	}
}

func createMetricsReceiver(
	_ context.Context,
	params receiver.Settings, // revive:disable-line:unused-parameter
	cfg component.Config,
	nextConsumer consumer.Metrics,
) (receiver.Metrics, error) {
	config := cfg.(*Config)
	return &Receiver{
		Config:     config,
		NextMetric: nextConsumer,
	}, nil
}
