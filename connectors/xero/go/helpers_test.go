// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	testAccessToken = "xero-test-access-token-0123456789"
	testContactID   = "025867f1-d741-4d6b-b1af-9ac774b59ba7"
	testInvoiceID   = "243216c5-369e-4056-ac67-05388f86dc81"
	testPaymentID   = "b26fd49a-cbae-470a-a8f8-bcbc119e0379"
	testTenantID    = "70784a63-d24b-46a9-a4db-0e70a274b056"

	// testClientIDPrefix and testClientIDSuffix keep the 32-hex fixture out of any single literal.
	testClientIDPrefix = "0F1E2D3C4B5A6978"
	testClientIDSuffix = "8796A5B4C3D2E1F0"
)

var xeroConnection = sdkgo.ConnectionRef{Provider: "xero", Name: "xero-test"}

type recordedRequest struct {
	method string
	path   string
	query  url.Values
	header http.Header
	body   string
}

// recordingXero is a credential-safe Xero fake that records every request and answers with reply.
type recordingXero struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
}

func newRecordingXero(t *testing.T, reply func(http.ResponseWriter, recordedRequest, int)) *recordingXero {
	t.Helper()
	provider := &recordingXero{}
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

func (provider *recordingXero) requestCount() int {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return len(provider.requests)
}

func (provider *recordingXero) request(index int) recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.requests[index]
}

func (provider *recordingXero) requestsTo(path string) []recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	var matching []recordedRequest
	for _, request := range provider.requests {
		if request.path == path {
			matching = append(matching, request)
		}
	}
	return matching
}

// newXeroClient connects with a Custom Connection access token, so no token or tenant request is made.
func newXeroClient(t *testing.T, providerURL string, options ...xero.Option) *xero.Client {
	t.Helper()
	client, err := xero.New(xero.Config{}, customConnectionCredentials(), append([]xero.Option{xero.WithLocalProviderURL(providerURL)}, options...)...)
	require.NoError(t, err)
	return client
}

// newOAuthClient connects with an OAuth access token and an organisation name or tenant ID.
func newOAuthClient(t *testing.T, providerURL string, organisation string) *xero.Client {
	t.Helper()
	client, err := xero.New(xero.Config{Organisation: organisation}, sdkgo.StaticCredentialProvider[xero.Credentials]{xeroConnection: {
		AuthMethodID: xero.OAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(testAccessToken),
	}}, xero.WithLocalProviderURL(providerURL))
	require.NoError(t, err)
	return client
}

func customConnectionCredentials() sdkgo.StaticCredentialProvider[xero.Credentials] {
	return sdkgo.StaticCredentialProvider[xero.Credentials]{xeroConnection: {
		AuthMethodID: xero.CustomConnectionAuthMethodID, ClientID: testClientIDPrefix + testClientIDSuffix,
		ClientSecret: sdkgo.NewSecretString("client-secret"), AccessToken: sdkgo.NewSecretString(testAccessToken),
	}}
}

func writeJSON(t *testing.T, response http.ResponseWriter, status int, body string) {
	t.Helper()
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Xero-Correlation-Id", "5fe9659e-e5cc-4747-ad01-47adb038bf34")
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

// dropConnection closes the connection after the request arrived, as a lost response would.
func dropConnection(t *testing.T, response http.ResponseWriter) {
	t.Helper()
	hijacker, isHijacker := response.(http.Hijacker)
	require.True(t, isHijacker)
	connection, _, err := hijacker.Hijack()
	require.NoError(t, err)
	require.NoError(t, connection.Close())
}

// invoiceJSON is a Xero invoice element with amounts as JSON numbers, as Xero sends them.
func invoiceJSON() map[string]any {
	return map[string]any{
		"Type": "ACCREC", "InvoiceID": testInvoiceID, "InvoiceNumber": "INV-0042", "Reference": "ORDER-1042",
		"Contact":    map[string]any{"ContactID": testContactID, "Name": "City Agency"},
		"DateString": "2026-10-01T00:00:00", "Date": "/Date(1759276800000+0000)/",
		"DueDateString": "2026-10-15T00:00:00", "DueDate": "/Date(1760486400000+0000)/", "Status": "AUTHORISED",
		"LineAmountTypes": "Exclusive", "CurrencyCode": "USD", "CurrencyRate": json.Number("1.0000000000"),
		"LineItems": []any{map[string]any{
			"LineItemID": "52208ff9-528a-4985-a9ad-b2b1d4210e38", "Description": "Spring bouquet", "Quantity": json.Number("2.0000"),
			"UnitAmount": json.Number("49.9500"), "AccountCode": "200", "TaxType": "OUTPUT", "TaxAmount": json.Number("9.99"),
			"LineAmount": json.Number("99.90"),
		}},
		"SubTotal": json.Number("99.90"), "TotalTax": json.Number("9.99"), "Total": json.Number("109.89"),
		"AmountDue": json.Number("109.89"), "AmountPaid": json.Number("0.00"), "AmountCredited": json.Number("0.00"),
		"UpdatedDateUTC": "/Date(1759309200000+0000)/", "HasErrors": false,
	}
}

func contactJSON(contactID string, email string, status string) map[string]any {
	return map[string]any{
		"ContactID": contactID, "ContactStatus": status, "Name": "City Agency", "FirstName": "Ana", "LastName": "Ortiz",
		"EmailAddress": email, "IsCustomer": "true", "IsSupplier": false, "DefaultCurrency": "USD",
		"UpdatedDateUTC": "/Date(1518685950940+0000)/",
	}
}

// xeroDexContext is a minimal dex.Context for running one operation outside a Worker.
type xeroDexContext struct {
	context.Context
	step string
}

func newXeroDexContext(step string) *xeroDexContext {
	return &xeroDexContext{Context: context.Background(), step: step}
}

func (*xeroDexContext) FlowID() string                          { return "xero-flow" }
func (*xeroDexContext) RunID() string                           { return "run" }
func (*xeroDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (context *xeroDexContext) StepExecutionID() string         { return context.step }
func (*xeroDexContext) FromStepExecutionID() string             { return "" }
func (*xeroDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*xeroDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (*xeroDexContext) Attempt() int32                          { return 1 }
func (*xeroDexContext) HasTimerFired() bool                     { return false }
func (*xeroDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*xeroDexContext) WaitForMethodFailed() bool               { return false }
func (*xeroDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*xeroDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*xeroDexContext) RecordEvent(string, any) error           { return nil }
func (*xeroDexContext) RecordHeartbeat(any) error               { return nil }
func (*xeroDexContext) GetLastHeartbeatValue(any) (bool, error) { return false, nil }

var _ dex.Context = (*xeroDexContext)(nil)
