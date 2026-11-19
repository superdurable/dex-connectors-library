// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lianlianpay

import (
	"crypto/rsa"
	"crypto/sha512"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"math/big"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const conformanceEndpointPath = ""
const conformanceFixturePathPrefix = ""

var conformanceKeyOnce sync.Once
var conformancePrivateKeyPEM string

func syntheticConformancePrivateKey() string {
	conformanceKeyOnce.Do(func() {
		exponent := big.NewInt(65537)
		for counter := uint64(0); ; counter++ {
			first := syntheticConformancePrime("first", counter)
			second := syntheticConformancePrime("second", counter)
			if first.Cmp(second) == 0 {
				continue
			}
			modulus := new(big.Int).Mul(first, second)
			if modulus.BitLen() != 1024 {
				continue
			}
			firstMinusOne := new(big.Int).Sub(first, big.NewInt(1))
			secondMinusOne := new(big.Int).Sub(second, big.NewInt(1))
			totient := new(big.Int).Mul(firstMinusOne, secondMinusOne)
			privateExponent := new(big.Int).ModInverse(exponent, totient)
			if privateExponent == nil {
				continue
			}
			key := &rsa.PrivateKey{PublicKey: rsa.PublicKey{N: modulus, E: 65537}, D: privateExponent, Primes: []*big.Int{first, second}}
			if key.Validate() != nil {
				continue
			}
			key.Precompute()
			conformancePrivateKeyPEM = string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
			return
		}
	})
	return conformancePrivateKeyPEM
}
func syntheticConformancePrime(label string, counter uint64) *big.Int {
	var suffix [8]byte
	binary.BigEndian.PutUint64(suffix[:], counter)
	first := sha512.Sum512(append([]byte("SYNTHETIC_CONFORMANCE_LIANLIANPAY_"+label), suffix[:]...))
	value := new(big.Int).SetBytes(first[:])
	value.SetBit(value, 511, 1)
	value.SetBit(value, 510, 1)
	value.SetBit(value, 0, 1)
	for !value.ProbablyPrime(32) {
		value.Add(value, big.NewInt(2))
	}
	return value
}
func newConformanceInvoker(t *testing.T, scenario conformanceScenario, endpoint string, transport *http.Client) conformanceInvoker {
	t.Helper()
	credentials := Credentials{MerchantID: "2020042200284052", PrivateKey: sdkgo.NewSecretString(syntheticConformancePrivateKey())}
	config := Config{BaseURL: endpoint}
	if scenario.Name == "queryPaymentStatus-oversized" {
		config.MaxResponseBytes = 16
	}
	client, err := New(config, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithHTTPClient(transport), clockOption{now: conformanceFixedClock(t, scenario)})
	require.NoError(t, err)
	return &conformanceMappedInvoker{t: t, scenario: scenario, actions: map[string]conformanceOperationInvocation{"queryPaymentStatus": newConformanceQueryInvocation(t, scenario.Input, queryContext{}, client.QueryPaymentStatus(), testConnection)}}
}
