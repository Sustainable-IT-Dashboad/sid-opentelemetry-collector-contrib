# Dynatrace Receiver for OpenTelemetry Collector

This is a custom OpenTelemetry receiver that pulls metrics from the Dynatrace Metrics API v2.

Its main goal is to fetch metrics from your existing Dynatrace setup and make them available to your OpenTelemetry pipeline — whether you want to log them, send them to dashboards, or route them to any exporter like Kafka or Prometheus.

To use it, just set up a .env file with your Dynatrace credentials and adjust the config.yaml based on the metrics you want to collect.

---

## Motivation

The goal is to use Dynatrace for performance monitoring and bring all their data into one central pipeline using OpenTelemetry.
This receiver does this by:

- Automatically fetching metrics from Dynatrace on a schedule  
- Transforming them into OpenTelemetry metrics  
- Allowing you to add custom labels (like environment or system ID)  
- Sending them to any OpenTelemetry exporter (Kafka, Prometheus, OTLP, etc.)

---

## Features

- Pulls metrics from Dynatrace Metrics API v2  
- Fully configurable via `config.yaml`  
- Compatible with any OpenTelemetry pipeline (processors/exporters)

---

## Example Configuration (`config.yaml`)

```yaml
receivers:
  dynatrace:
    API_ENDPOINT: ${env:API_ENDPOINT}
    API_TOKEN: ${env:API_TOKEN}
    metric_selectors:
      - builtin:containers.cpu.usageTime
      - builtin:containers.memory.residentSetBytes
    resolution: 1h
    from: now-1h
    to: now
    poll_interval: 30s
    max_retries: 3
    http_timeout: 10s

processors:
  batch:
  resource:
    attributes:
      - key: environment
        value: ${env:DEPLOYMENT_ENVIRONMENT}
        action: upsert
      - key: project_name
        value: ${env:PROJECT_NAME}
        action: upsert
      - key: team_owner
        value: "team-sid"
        action: insert

exporters:
  logging:
    verbosity: detailed

service:
  pipelines:
    metrics:
      receivers: [dynatrace]
      processors: [batch, resource]
      exporters: [logging]

```

You can use `custom_labels` to tag metrics with helpful context like environment (`prod`, `stage`) or system identifiers.
The idea is to make it as simple as possible and adjust needs with the config.yaml


You can use .env variables to inject values into your config dynamically:

## .env

```
API_ENDPOINT=https://your-tenant.live.dynatrace.com/api/v2/metrics/query
API_TOKEN=dt0c01.XXX
DEPLOYMENT_ENVIRONMENT=prod
PROJECT_NAME=my-app
```

## Quick Notes

This receiver runs on a polling schedule (poll_interval) and queries a time range based on from and to.
Dynatrace supports relative times like now-1h and now for simple configuration. The receiver does not require any special Dynatrace agent or ActiveGate.

## Getting Started

Copy your .env and config.yaml to your working directory

Build your custom Collector using the OpenTelemetry Collector Builder

Run it with:

bash
```
./otelcontribcol_windows_amd64.exe --config=receiver/dynatracereceiver/config.yaml
```
---


