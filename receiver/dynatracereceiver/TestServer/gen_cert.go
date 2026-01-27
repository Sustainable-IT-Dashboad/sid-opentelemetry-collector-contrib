package main

import (
    "crypto/rand"
    "crypto/rsa"
    "crypto/x509"
    "crypto/x509/pkix"
    "encoding/pem"
    "math/big"
    "net"
    "os"
    "time"
)

func main() {
    key, err := rsa.GenerateKey(rand.Reader, 2048)
    if err != nil {
        panic(err)
    }

    serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
    if err != nil {
        panic(err)
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
        panic(err)
    }

    certOut, err := os.Create("cert.pem")
    if err != nil {
        panic(err)
    }
    defer certOut.Close()
    if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: derBytes}); err != nil {
        panic(err)
    }

    keyOut, err := os.Create("key.pem")
    if err != nil {
        panic(err)
    }
    defer keyOut.Close()
    privBytes := x509.MarshalPKCS1PrivateKey(key)
    if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes}); err != nil {
        panic(err)
    }

    println("Wrote cert.pem and key.pem")
}
