package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"os"
	"time"
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
    // ensure cert/key exist (generate self-signed if missing)
    if _, err := os.Stat("cert.pem"); os.IsNotExist(err) {
        if err := genAndWriteSelfSignedCert("cert.pem", "key.pem"); err != nil {
            panic(fmt.Errorf("failed to generate certs: %w", err))
        }
    }

    http.HandleFunc("/api/v2/metrics/query", handler)
    fmt.Println("Starting HTTPS test server on https://localhost:8443/api/v2/metrics/query")
    if err := http.ListenAndServeTLS(":8443", "cert.pem", "key.pem", nil); err != nil {
        panic(err)
    }
}

func genAndWriteSelfSignedCert(certPath, keyPath string) error {
    key, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        return err
    }

    serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
    if err != nil {
        return err
    }

    tmpl := x509.Certificate{
        SerialNumber: serial,
        Subject: pkix.Name{
            Organization: []string{"local-test"},
        },
        NotBefore: time.Now().Add(-time.Hour),
        NotAfter:  time.Now().AddDate(1, 0, 0),
        KeyUsage:  x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
        ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
        BasicConstraintsValid: true,
        IPAddresses: []net.IP{net.ParseIP("127.0.0.1")},
        DNSNames:    []string{"localhost"},
    }

    derBytes, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
    if err != nil {
        return err
    }

    certOut, err := os.Create(certPath)
    if err != nil {
        return err
    }
    defer certOut.Close()
    if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
        return err
    }

    keyOut, err := os.Create(keyPath)
    if err != nil {
        return err
    }
    defer keyOut.Close()
    privBytes := x509.MarshalPKCS1PrivateKey(key)
    if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes}); err != nil {
        return err
    }

    fmt.Println("Wrote cert.pem and key.pem")
    return nil
}
