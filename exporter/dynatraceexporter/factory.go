// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dynatraceexporter

import (
	"context"
	"net/http"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

const TypeStr = "dynatraceexporter"

func createDefaultConfig() component.Config {
	return &Config{
		Endpoint: "", // no default; must be set to the OTLP HTTP endpoint
	}
}

func NewFactory() exporter.Factory {
	return exporter.NewFactory(
		component.MustNewType("dynatraceexporter"), //  "dynatrace_otlphttp",  // exporter type key in config
		createDefaultConfig,
		exporter.WithMetrics(createMetricsExporter, component.StabilityLevelDevelopment), // component.StabilityLevelBeta
	)
}

func createMetricsExporter(
	ctx context.Context,
	set exporter.Settings, // settings (incl. Telemetry, Logger, BuildInfo)
	cfg component.Config,
) (exporter.Metrics, error) {
	conf := cfg.(*Config)
	// Create the exporter instance (initialize HTTP client, etc.)
	exp := &Exporter{
		Endpoint: conf.Endpoint,
		Client:   &http.Client{}, // (configure timeouts as needed)
	}
	// Use exporterhelper to wrap our push function with queue/retry support.
	return exporterhelper.NewMetrics(ctx, set, cfg, exp.PushMetrics) // You can add options here, e.g., exporterhelper.WithQueue(...)

}
