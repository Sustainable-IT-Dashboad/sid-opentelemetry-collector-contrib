// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dynatraceexporter

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/consumer"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"go.opentelemetry.io/collector/pdata/pmetric/pmetricotlp"
)

type Exporter struct {
	Endpoint string
	Client   *http.Client
}

func NewSimpleExporter() *Exporter {
	return &Exporter{}
}

func (e *Exporter) Capabilities() consumer.Capabilities {
	return consumer.Capabilities{MutatesData: false}
}

func (e *Exporter) Start(_ context.Context, _ component.Host) error {
	fmt.Println("Simple Exporter started. Target:", e.Endpoint)
	return nil
}

func (e *Exporter) Shutdown(_ context.Context) error {
	fmt.Println("Simple Exporter shutting down.")
	return nil
}

func (e *Exporter) ConsumeMetrics(ctx context.Context, md pmetric.Metrics) error {
	fmt.Println("Simple Exporter received metrics:", md.MetricCount())
	fmt.Println(md) // Just for testing, print the incoming data from dynatracereceiver

	return e.PushMetrics(ctx, md)
}

func (ce *Exporter) PushMetrics(ctx context.Context, md pmetric.Metrics) error {

	req := pmetricotlp.NewExportRequestFromMetrics(md)

	payload, err := req.MarshalProto() // Marshal to bytes (protobuf):
	if err != nil {
		return err
	}

	// Build the HTTP POST request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", ce.Endpoint, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	httpReq.Header.Set("Content-Type", "application/x-protobuf")
	httpReq.Header.Set("Content-Encoding", "identity") // no compression

	// Send the request
	resp, err := ce.Client.Do(httpReq)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return fmt.Errorf("failed to POST metrics (status %d)", resp.StatusCode)
	}
	return nil
}
