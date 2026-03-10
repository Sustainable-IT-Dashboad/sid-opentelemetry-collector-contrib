// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dynatraceexporter_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Sustainable-IT-Dashboad/sid-opentelemetry-collector-contrib/exporter/dynatraceexporter"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/collector/pdata/pmetric"
)

func createTestMetrics() pmetric.Metrics {
	md := pmetric.NewMetrics()
	rm := md.ResourceMetrics().AppendEmpty()
	sm := rm.ScopeMetrics().AppendEmpty()
	m := sm.Metrics().AppendEmpty()
	m.SetName("test.metric")
	m.SetEmptyGauge().DataPoints().AppendEmpty().SetDoubleValue(123.4)
	return md
}

func TestPushMetrics(t *testing.T) {
	// Step 1: Start a fake HTTP server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "application/x-protobuf", r.Header.Get("Content-Type"))
		require.Equal(t, http.MethodPost, r.Method)
		w.WriteHeader(http.StatusAccepted) // Simulate success
	}))
	defer server.Close()

	// Step 2: Setup exporter
	exporter := &dynatraceexporter.Exporter{
		Client:   server.Client(),
		Endpoint: server.URL,
	}

	// Step 3: Send test metrics
	metrics := createTestMetrics()
	err := exporter.PushMetrics(context.Background(), metrics)
	require.NoError(t, err)
}
