// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package lianlianpay

import (
	"context"
	"crypto"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "lianlianpay", Name: "lianlianpay"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "lianlianpay-provider-test" }
func (queryContext) StepExecutionID() string     { return "payment-status" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, privateKey string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {MerchantID: "2020042200284052", PrivateKey: sdkgo.NewSecretString(privateKey)}})
	require.NoError(t, err)
	return client
}
func testPrivateKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	require.NoError(t, err)
	encoded := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	return key, string(encoded)
}
func runPayment(t *testing.T, server *httptest.Server, privateKey string) (sdkgo.QueryResult[PaymentStatusResult], error) {
	t.Helper()
	return sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, privateKey).QueryPaymentStatus(), testConnection, QueryPaymentStatusInput{MerchantOrderID: "ORDER123"})
}
func TestOfficialMD5WithRSASignatureRecipe(t *testing.T) {
	key, _ := testPrivateKey(t)
	body := []byte(`{"card_no":"9558834301000461394","mch_id":"2020042200284052"}`)
	signature, err := signPaymentRequest(key, body)
	require.NoError(t, err)
	decoded, err := base64.StdEncoding.DecodeString(signature)
	require.NoError(t, err)
	first := md5.Sum(body)
	require.Equal(t, "51a592df873c64dd2c2c88ce8c365ec7", hex.EncodeToString(first[:]))
	second := md5.Sum([]byte(hex.EncodeToString(first[:])))
	require.NoError(t, rsa.VerifyPKCS1v15(&key.PublicKey, crypto.MD5, second[:], decoded))
	require.Error(t, rsa.VerifyPKCS1v15(&key.PublicKey, crypto.MD5, first[:], decoded))
}
func TestQueryPaymentStatusSignedRequestAndFilteredOutput(t *testing.T) {
	key, privateKey := testPrivateKey(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/query/mch/v1/accp/txn/order-query", r.URL.Path)
		require.Empty(t, r.URL.RawQuery)
		require.Equal(t, "RSA", r.Header.Get("Signature-Type"))
		require.Equal(t, "2020042200284052", r.Header.Get("mch_id"))
		require.Len(t, r.Header.Get("timestamp"), 14)
		require.Equal(t, time.Now().In(time.FixedZone("CST", 8*60*60)).Format("20060102"), r.Header.Get("timestamp")[:8])
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"mch_id":"2020042200284052","txn_seqno":"ORDER123"}`, string(body))
		signature, err := base64.StdEncoding.DecodeString(r.Header.Get("Signature-Data"))
		require.NoError(t, err)
		first := md5.Sum(body)
		second := md5.Sum([]byte(hex.EncodeToString(first[:])))
		require.NoError(t, rsa.VerifyPKCS1v15(&key.PublicKey, crypto.MD5, second[:], signature))
		_, err = w.Write([]byte(`{"ret_code":"0000","txn_seqno":"ORDER123","txn_status":"SUCCESS","user_id":"private payer","bank_account":"private account"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := runPayment(t, server, privateKey)
	require.NoError(t, err)
	require.Equal(t, QueryPaymentStatusBranchFound, result.Branch)
	require.Equal(t, PaymentStatusResult{MerchantOrderID: "ORDER123", Status: "SUCCESS"}, result.Value)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
}
func TestQueryPaymentStatusOutcomes(t *testing.T) {
	_, privateKey := testPrivateKey(t)
	cases := []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"rejected", 200, `{"ret_code":"9999"}`, QueryPaymentStatusBranchProviderRejected, false},
		{"limited", 429, `{}`, "", true},
		{"unavailable", 503, `{}`, "", true},
		{"malformed", 200, `{`, QueryPaymentStatusBranchInvalidResponse, false},
		{"missing status", 200, `{"ret_code":"0000","txn_seqno":"ORDER123"}`, QueryPaymentStatusBranchInvalidResponse, false},
		{"wrong order", 200, `{"ret_code":"0000","txn_seqno":"OTHER","txn_status":"SUCCESS"}`, QueryPaymentStatusBranchInvalidResponse, false},
		{"unauthorized", 401, `{}`, QueryPaymentStatusBranchProviderRejected, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer server.Close()
			result, err := runPayment(t, server, privateKey)
			if tc.retry {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				require.Equal(t, tc.branch, result.Branch)
			}
		})
	}
}
func TestInvalidInputAndCredentialsDoNotCallProvider(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls.Add(1) }))
	defer server.Close()
	_, privateKey := testPrivateKey(t)
	client := newTestClient(t, server.URL, privateKey)
	result, err := sdkgo.RunQuery(queryContext{}, client.QueryPaymentStatus(), testConnection, QueryPaymentStatusInput{MerchantOrderID: " "})
	require.NoError(t, err)
	require.Equal(t, QueryPaymentStatusBranchDefect, result.Branch)
	client = newTestClient(t, server.URL, "not PEM")
	result, err = sdkgo.RunQuery(queryContext{}, client.QueryPaymentStatus(), testConnection, QueryPaymentStatusInput{MerchantOrderID: "ORDER123"})
	require.NoError(t, err)
	require.Equal(t, QueryPaymentStatusBranchDefect, result.Branch)
	require.Zero(t, calls.Load())
}
func TestRedirectDoesNotForwardMerchantCredentials(t *testing.T) {
	var forwarded atomic.Bool
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { forwarded.Store(true) }))
	defer destination.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, destination.URL, http.StatusFound) }))
	defer server.Close()
	_, privateKey := testPrivateKey(t)
	result, err := runPayment(t, server, privateKey)
	require.NoError(t, err)
	require.Equal(t, QueryPaymentStatusBranchProviderRejected, result.Branch)
	require.False(t, forwarded.Load())
	require.False(t, strings.Contains(result.Failure.Message, "PRIVATE KEY"))
}
