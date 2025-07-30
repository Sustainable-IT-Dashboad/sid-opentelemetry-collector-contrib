// Copyright The OpenTelemetry Authors
// SPDX-License-Identifier: Apache-2.0

package dynatraceexporter

import (
	"go.opentelemetry.io/collector/component"
	"go.opentelemetry.io/collector/exporter/exporterhelper"
)

type Config struct {
	exporterhelper.TimeoutConfig `mapstructure:",squash"`
	Endpoint                     string `mapstructure:"endpoint"`
	component.Config             `mapstructure:",squash"`
}
