// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testAccessToken = "intuit-test-access-token-0123456789"
	testClientID    = "ABtestQuickBooksClientID0123456789"
	testRealmID     = "9341453050298464"
	testCustomerID  = "58"
	testInvoiceID   = "130"
	testPaymentID   = "186"
	testIntuitTID   = "1-66f9a1c2-5b3e4f7a2d1c0b9e8f7a6b5c"
	companyPath     = "/v3/company/" + testRealmID
)

var quickbooksConnection = sdkgo.ConnectionRef{Provider: "quickbooks", Name: "quickbooks-test"}

type recordedRequest struct {
	method string
	path   string
	query  url.Values
	header http.Header
	body   string
}

// recordingQuickBooks is a credential-safe QuickBooks fake that records every request and answers with reply.
type recordingQuickBooks struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingQuickBooks(t *testing.T, reply func(http.ResponseWriter, recordedRequest, int)) *recordingQuickBooks {
	t.Helper()
	provider := &recordingQuickBooks{}
	provider.Server = httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		recorded := recordedRequest{
			method: request.Method, path: request.URL.Path, query: request.URL.Query(), header: request.Header.Clone(), body: string(body),
		}
		provider.mutex.Lock()
		index := len(provider.requests)
		provider.requests = append(provider.requests, recorded)
		provider.mutex.Unlock()
		reply(response, recorded, index)
	}))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *recordingQuickBooks) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingQuickBooks) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

// newQuickBooksClient connects with an access token and an ID token naming testRealmID, so no refresh is made.
func newQuickBooksClient(t *testing.T, providerURL string, options ...quickbooks.Option) *quickbooks.Client {
	t.Helper()
	client, err := quickbooks.New(quickbooks.Config{}, staticCredentials(testIDToken(t, `"realmid":"`+testRealmID+`"`)),
		append([]quickbooks.Option{quickbooks.WithLocalProviderURL(providerURL)}, options...)...)
	require.NoError(t, err)
	return client
}

func staticCredentials(idToken string) sdkgo.StaticCredentialProvider[quickbooks.Credentials] {
	return sdkgo.StaticCredentialProvider[quickbooks.Credentials]{quickbooksConnection: {
		ClientID: testClientID, ClientSecret: sdkgo.NewSecretString("client-secret"),
		AccessToken: sdkgo.NewSecretString(testAccessToken), IDToken: sdkgo.NewSecretString(idToken),
	}}
}

// testIDToken is an unsigned Intuit-shaped ID token whose payload adds realmClaim, such as "realmid":"1".
func testIDToken(t *testing.T, realmClaim string) string {
	t.Helper()
	claims := `{"sub":"8b9f5a10","aud":["` + testClientID + `"],"iss":"https://oauth.platform.intuit.com/op/v1","exp":1759312800`
	if realmClaim != "" {
		claims += "," + realmClaim
	}
	claims += "}"
	return encodedIDToken(claims)
}

// encodedIDToken is an unsigned JWT with the given payload.
func encodedIDToken(claims string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256"}`)) + "." + base64.RawURLEncoding.EncodeToString([]byte(claims)) + ".c2ln"
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json;charset=UTF-8")
	response.Header().Set("intuit_tid", testIntuitTID)
	response.WriteHeader(status)
	_, err := io.WriteString(response, body)
	require.NoError(t, err)
}

func writeValue(t *testing.T, response http.ResponseWriter, status int, value any) {
	t.Helper()
	encoded, err := json.Marshal(value)
	require.NoError(t, err)
	writeJSON(t, response, status, string(encoded))
}

// faultJSON is a QuickBooks Fault whose Message and Detail carry SENTINEL text that must never surface.
func faultJSON(faultType string, code string) string {
	return `{"Fault":{"Error":[{"Message":"SENTINEL message","Detail":"SENTINEL detail","code":"` + code +
		`","element":"DisplayName"}],"type":"` + faultType + `"},"time":"2026-10-01T02:00:00.000-07:00"}`
}

// dropConnection closes the connection after the request arrived, as a lost response would.
func dropConnection(t *testing.T, response http.ResponseWriter) {
	t.Helper()
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(t, err)
	require.NoError(t, connection.Close())
}

// invoiceJSON is a QuickBooks invoice with amounts as JSON numbers and the subtotal line QuickBooks adds.
func invoiceJSON() map[string]any {
	return map[string]any{
		"Id": testInvoiceID, "SyncToken": "2", "DocNumber": "ORDER-1042", "TxnDate": "2026-10-01", "DueDate": "2026-10-15",
		"CustomerRef":  map[string]any{"value": testCustomerID, "name": "City Agency"},
		"CurrencyRef":  map[string]any{"value": "USD", "name": "United States Dollar"},
		"ExchangeRate": json.Number("1"),
		"Line": []any{
			map[string]any{"Id": "1", "LineNum": 1, "Description": "Spring bouquet", "Amount": json.Number("99.90"), "DetailType": "SalesItemLineDetail",
				"SalesItemLineDetail": map[string]any{"ItemRef": map[string]any{"value": "1", "name": "Services"}, "Qty": json.Number("2"),
					"UnitPrice": json.Number("49.95"), "TaxCodeRef": map[string]any{"value": "NON"}, "ServiceDate": "2026-09-30"}},
			map[string]any{"Amount": json.Number("99.90"), "DetailType": "SubTotalLineDetail", "SubTotalLineDetail": map[string]any{}},
		},
		"TotalAmt": json.Number("99.90"), "Balance": json.Number("0"), "TxnTaxDetail": map[string]any{"TotalTax": json.Number("0")},
		"EmailStatus": "EmailSent", "BillEmail": map[string]any{"Address": "accounts@cityagency.example.com"},
		"DeliveryInfo": map[string]any{"DeliveryType": "Email", "DeliveryTime": "2026-10-01T02:05:00-07:00"},
		"CustomerMemo": map[string]any{"value": "Thank you"}, "PrivateNote": "Order ORDER-1042",
		"LinkedTxn": []any{map[string]any{"TxnId": testPaymentID, "TxnType": "Payment"}},
		"MetaData":  map[string]any{"CreateTime": "2026-10-01T02:00:00-07:00", "LastUpdatedTime": "2026-10-01T02:05:00-07:00"},
		"domain":    "QBO", "sparse": false,
	}
}

func customerJSON(customerID string, displayName string, email string, isActive bool) map[string]any {
	return map[string]any{
		"Id": customerID, "SyncToken": "0", "DisplayName": displayName, "GivenName": "Ana", "FamilyName": "Ortiz",
		"CompanyName": "City Agency LLC", "PrimaryEmailAddr": map[string]any{"Address": email},
		"PrimaryPhone": map[string]any{"FreeFormNumber": "(555) 555-0100"}, "Active": isActive, "Balance": json.Number("0"),
		"CurrencyRef": map[string]any{"value": "USD"},
		"MetaData":    map[string]any{"CreateTime": "2026-09-01T08:00:00-07:00", "LastUpdatedTime": "2026-09-02T08:00:00-07:00"},
	}
}

// quickbooksDexContext is a minimal dex.Context for running one operation outside a Worker.
type quickbooksDexContext struct {
	context.Context
	step string
}

func newQuickBooksDexContext(step string) *quickbooksDexContext {
	return &quickbooksDexContext{Context: context.Background(), step: step}
}

func (*quickbooksDexContext) FlowID() string                          { return "quickbooks-flow" }
func (*quickbooksDexContext) RunID() string                           { return "run" }
func (*quickbooksDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (context *quickbooksDexContext) StepExecutionID() string         { return context.step }
func (*quickbooksDexContext) FromStepExecutionID() string             { return "" }
func (*quickbooksDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*quickbooksDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (*quickbooksDexContext) Attempt() int32                          { return 1 }
func (*quickbooksDexContext) HasTimerFired() bool                     { return false }
func (*quickbooksDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*quickbooksDexContext) WaitForMethodFailed() bool               { return false }
func (*quickbooksDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*quickbooksDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*quickbooksDexContext) RecordEvent(string, any) error           { return nil }
func (*quickbooksDexContext) RecordHeartbeat(any) error               { return nil }
func (*quickbooksDexContext) GetLastHeartbeatValue(any) (bool, error) { return false, nil }

var _ dex.Context = (*quickbooksDexContext)(nil)
