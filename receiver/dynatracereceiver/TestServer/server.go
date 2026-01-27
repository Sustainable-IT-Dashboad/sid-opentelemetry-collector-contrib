package main

import (
    "encoding/json"
    "net/http"
    "time"
    "fmt"
)

type DynatraceResponse struct {
    TotalCount  int                    `json:"totalCount"`
    NextPageKey string                 `json:"nextPageKey"`
    Resolution  string                 `json:"resolution"`
    Result      []DynatraceMetricData  `json:"result"`
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

func handler(w http.ResponseWriter, r *http.Request) {
    nowMs := time.Now().UnixMilli()
    resp := DynatraceResponse{
        TotalCount: 1,
        NextPageKey: "",
        Resolution: "Inf",
        Result: []DynatraceMetricData{
            {
                MetricID: "custom.metric.test",
                Data: []MetricValues{
                    {
                        Timestamps: []int64{nowMs},
                        Values: []float64{123.45},
                        Dimensions: []string{},
                        DimensionMap: map[string]string{"host":"local"},
                    },
                },
            },
        },
    }

    b, err := json.Marshal(resp)
    if err != nil {
        http.Error(w, "marshal error", http.StatusInternalServerError)
        return
    }

    w.Header().Set("Content-Type", "application/json")
    fmt.Fprintln(w, string(b))
}

func main() {
    http.HandleFunc("/api/v2/metrics/query", handler)
    fmt.Println("Starting HTTPS test server on https://localhost:8443/api/v2/metrics/query")
    if err := http.ListenAndServeTLS(":8443", "cert.pem", "key.pem", nil); err != nil {
        panic(err)
    }
}
