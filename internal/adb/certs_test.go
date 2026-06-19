package adb

import (
	"crypto/x509"
	"encoding/pem"
	"testing"
)

// A self-signed CA with subject "CN=adbq Test CA, O=adbq". OpenSSL reports its
// subject_hash_old as eb1bf87a — this locks in our Android trust-store filename
// computation against the canonical implementation.
const testCertPEM = `-----BEGIN CERTIFICATE-----
MIIDLTCCAhWgAwIBAgIUZr5xQqpQ2Q3mmTuF27QQ0RYCCEgwDQYJKoZIhvcNAQEL
BQAwJjEVMBMGA1UEAwwMYWRicSBUZXN0IENBMQ0wCwYDVQQKDARhZGJxMB4XDTI2
MDYxODEyMDIxMFoXDTI3MDYxODEyMDIxMFowJjEVMBMGA1UEAwwMYWRicSBUZXN0
IENBMQ0wCwYDVQQKDARhZGJxMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKC
AQEAxSO08LbIMhL5S0WJfhZ8Pur5EaOvL/V4qhNRBDPy6it6KFX6kGDkfGz7dqr6
WUTfRs9uwlTk3zoyUUcOvTSncMbMyEvvnovb1NliXiEEbcQLxgmpjefhJRgCK3GM
wQusEPgwI2Foq1pjGC6nQtS8L/A1lZOygsc2ZQ4GKKKhdjVFrR59gARALfCJ4RI6
HZZDu/X9J3ASoCyZ/u7SvUJvA/aIpV0JlvyTwzdrmvuT9QaFtx8uL/UAAuTrcF6I
11sbnksE260vgM1StXWkbrqWRqXkkGYlGd2PYcWD9AFK8L0fGJQMRtwt1XLM5l2j
RJs/Z134L/rteOJHHDqrdNY5QQIDAQABo1MwUTAdBgNVHQ4EFgQUmvlsA/NprnVR
MJcQKRHnUiHlNgswHwYDVR0jBBgwFoAUmvlsA/NprnVRMJcQKRHnUiHlNgswDwYD
VR0TAQH/BAUwAwEB/zANBgkqhkiG9w0BAQsFAAOCAQEAL5jsUJcLGyncr3H/cA9y
motDq8K6ik7YhQcUlS32FYrSEelqDUdoIxAC0H4OaoN+MydX26hVH0weWFFqtLoa
4UgrWzvuJdCS363cWur49wLOYke96h8buOtLPJfn9ZZf149CtRvJGGJlG+b3BdnU
UbBfUQhZ8hpc+mUw5k0oca3KbEiuwOguLL3XGAspWwL+5c8qMsd9BsRI3+mwqjuf
LxiZL+m3PQGXncAunhUfvSmqrnOznIYHYCfR26E1Fy4MBA2LljcSZYucyOAH+bQR
HpA1yDnI3Acq2C0Tadqu+LdzEku1uPXKiI0HJbJ63diirbojkiT59SJ2O25MnbN6
gg==
-----END CERTIFICATE-----`

func TestAndroidSubjectHash(t *testing.T) {
	ca, ok := parseCACertPEM(testCertPEM)
	if !ok {
		t.Fatal("parseCACertPEM failed")
	}
	if ca.Subject != "adbq Test CA" {
		t.Errorf("subject = %q, want %q", ca.Subject, "adbq Test CA")
	}
	if !ca.SelfSigned {
		t.Error("expected self-signed")
	}

	block, _ := pem.Decode([]byte(testCertPEM))
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		t.Fatal(err)
	}
	if got := androidSubjectHash(cert); got != "eb1bf87a" {
		t.Errorf("androidSubjectHash = %s, want eb1bf87a (openssl subject_hash_old)", got)
	}
}
