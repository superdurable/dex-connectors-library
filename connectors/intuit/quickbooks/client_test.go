// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks_test

import (
	"errors"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

// hostRecordingTransport answers every request itself and records the URL the connector built.
type hostRecordingTransport struct {
	urls *[]string
}

func (transport hostRecordingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	*transport.urls = append(*transport.urls, request.URL.String())
	return nil, errors.New("recorded without sending")
}

func TestEnvironmentSelectsTheQuickBooksHostAndEveryRequestPinsTheMinorVersion(t *testing.T) {
	for environment, host := range map[quickbooks.Environment]string{
		"": "quickbooks.api.intuit.com", quickbooks.EnvironmentProduction: "quickbooks.api.intuit.com",
		quickbooks.EnvironmentSandbox: "sandbox-quickbooks.api.intuit.com",
	} {
		var urls []string
		client, err := quickbooks.New(quickbooks.Config{Environment: environment}, staticCredentials(testIDToken(t, `"realmid":"`+testRealmID+`"`)),
			quickbooks.WithHTTPClient(&http.Client{Transport: hostRecordingTransport{urls: &urls}}))
		require.NoError(t, err)
		_, err = sdkgo.RunQuery(newQuickBooksDexContext("host"), client.GetInvoice(), quickbooksConnection, quickbooks.GetInvoiceInput{InvoiceID: testInvoiceID})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "a request that never reached QuickBooks is retried")
		require.Equal(t, []string{"https://" + host + companyPath + "/invoice/" + testInvoiceID + "?minorversion=75"}, urls, environment)
	}
	_, err := quickbooks.New(quickbooks.Config{Environment: "staging"}, staticCredentials(""))
	require.Error(t, err, "the environment is a validated choice, never a typed URL")
}

func TestStatusesMapToBranchesWithoutQuickBooksText(t *testing.T) {
	for _, test := range []struct {
		name      string
		status    int
		body      string
		branch    sdkgo.BranchID
		kind      sdkgo.FailureKind
		faultCode string
		retry     bool
	}{
		{name: "missing invoice", status: http.StatusBadRequest, body: faultJSON("ValidationFault", "610"),
			branch: quickbooks.GetInvoiceBranchNotFound, kind: sdkgo.FailureNotFound, faultCode: "610"},
		{name: "missing invoice with HTTP 200", status: http.StatusOK, body: faultJSON("ValidationFault", "610"),
			branch: quickbooks.GetInvoiceBranchNotFound, kind: sdkgo.FailureNotFound, faultCode: "610"},
		{name: "unknown path", status: http.StatusNotFound, body: `SENTINEL`, branch: quickbooks.GetInvoiceBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "expired token", status: http.StatusUnauthorized, body: faultJSON("AuthenticationFault", "3200"),
			branch: quickbooks.GetInvoiceBranchProviderRejected, kind: sdkgo.FailureAuthentication, faultCode: "3200"},
		{name: "insufficient scope", status: http.StatusForbidden, body: faultJSON("AuthorizationFault", "3100"),
			branch: quickbooks.GetInvoiceBranchProviderRejected, kind: sdkgo.FailureAuthorization, faultCode: "3100"},
		{name: "business validation", status: http.StatusBadRequest, body: faultJSON("ValidationFault", "6000"),
			branch: quickbooks.GetInvoiceBranchProviderRejected, kind: sdkgo.FailureValidation, faultCode: "6000"},
		{name: "redirect", status: http.StatusFound, body: `{}`, branch: quickbooks.GetInvoiceBranchProviderRejected, kind: sdkgo.FailureProtocol},
		{name: "not implemented", status: http.StatusNotImplemented, body: `SENTINEL`, branch: quickbooks.GetInvoiceBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
		{name: "credential reflected", status: http.StatusOK, body: `{"Invoice":{"PrivateNote":"` + testAccessToken + `"}}`,
			branch: quickbooks.GetInvoiceBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "not JSON", status: http.StatusOK, body: `<IntuitResponse/>`, branch: quickbooks.GetInvoiceBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "system fault", status: http.StatusOK, body: faultJSON("SystemFault", "10000"), kind: sdkgo.FailureAvailability, retry: true},
		{name: "internal error", status: http.StatusInternalServerError, body: `{}`, kind: sdkgo.FailureAvailability, retry: true},
		{name: "unavailable", status: http.StatusServiceUnavailable, body: `SENTINEL`, kind: sdkgo.FailureAvailability, retry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
				if test.status == http.StatusFound {
					response.Header().Set("Location", "https://elsewhere.example.com/")
				}
				writeJSON(t, response, test.status, test.body)
			})
			result, err := sdkgo.RunQuery(newQuickBooksDexContext("status-"+test.name), newQuickBooksClient(t, provider.URL).GetInvoice(),
				quickbooksConnection, quickbooks.GetInvoiceInput{InvoiceID: testInvoiceID})
			if test.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, test.kind, retry.Failure.Kind)
				require.NotContains(t, retry.Failure.Message, "SENTINEL")
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			require.Equal(t, test.kind, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "SENTINEL")
			require.NotContains(t, result.Failure.Message, testAccessToken)
			require.Equal(t, testIntuitTID, result.Receipt.ProviderRequestID)
			require.Equal(t, testRealmID, result.Receipt.Metadata[quickbooks.RealmIDReceiptKey])
			require.Equal(t, test.faultCode, result.Receipt.Metadata[quickbooks.FaultCodeReceiptKey])
			require.Equal(t, 1, provider.requestCount(), "a conclusive answer is not resent")
		})
	}
}

func TestThrottlingWaitsForRetryAfterOrTheDocumentedMinute(t *testing.T) {
	for retryAfterHeader, delay := range map[string]time.Duration{"7": 7 * time.Second, "": time.Minute} {
		provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
			if retryAfterHeader != "" {
				response.Header().Set("Retry-After", retryAfterHeader)
			}
			writeJSON(t, response, http.StatusTooManyRequests, faultJSON("ThrottlingFault", "003001"))
		})
		_, err := sdkgo.RunQuery(newQuickBooksDexContext("throttled"), newQuickBooksClient(t, provider.URL).ListInvoices(), quickbooksConnection,
			quickbooks.ListInvoicesInput{})
		var retryAfter *dex.RetryAfterError
		require.ErrorAs(t, err, &retryAfter)
		require.Equal(t, delay, retryAfter.After)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
	}
}

func TestNewValidatesConfigurationAndTheLocalProviderURL(t *testing.T) {
	credentials := staticCredentials("")
	_, err := quickbooks.New(quickbooks.Config{}, nil)
	require.Error(t, err)
	_, err = quickbooks.New(quickbooks.Config{MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	for _, realmID := range []string{"company", "0123", "93414530502984649341453050298464"} {
		_, err = quickbooks.New(quickbooks.Config{RealmID: realmID}, credentials)
		require.Error(t, err, realmID)
	}
	_, err = quickbooks.New(quickbooks.Config{}, credentials, nil)
	require.Error(t, err)
	for _, providerURL := range []string{"https://quickbooks.api.intuit.com", "http://192.0.2.10:8080", "http://127.0.0.1:8080/v3", "ftp://127.0.0.1"} {
		_, err = quickbooks.New(quickbooks.Config{}, credentials, quickbooks.WithLocalProviderURL(providerURL))
		require.Error(t, err, providerURL)
	}
	client, err := quickbooks.New(quickbooks.Config{RealmID: " " + testRealmID + " "}, credentials, quickbooks.WithLocalProviderURL("http://localhost:8080"))
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestTransportFailuresRetryAndNeverReachAnotherHost(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { dropConnection(t, response) })
	_, err := sdkgo.RunQuery(newQuickBooksDexContext("dropped"), newQuickBooksClient(t, provider.URL).GetInvoice(), quickbooksConnection,
		quickbooks.GetInvoiceInput{InvoiceID: testInvoiceID})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)

	refused := newRecordingQuickBooks(t, func(http.ResponseWriter, recordedRequest, int) {})
	refusedURL := refused.URL
	refused.Close()
	_, err = sdkgo.RunQuery(newQuickBooksDexContext("refused"), newQuickBooksClient(t, refusedURL).GetInvoice(), quickbooksConnection,
		quickbooks.GetInvoiceInput{InvoiceID: testInvoiceID})
	require.ErrorAs(t, err, &retry)
	require.Contains(t, retry.Failure.Message, "no request was sent")
}

func TestOversizedResponseIsInvalidForAReadAndUncertainForAWrite(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusOK, `{"Invoice":{"PrivateNote":"`+strings.Repeat("x", 2048)+`"}}`)
	})
	client, err := quickbooks.New(quickbooks.Config{MaxResponseBytes: 1024}, staticCredentials(testIDToken(t, `"realmid":"`+testRealmID+`"`)),
		quickbooks.WithLocalProviderURL(provider.URL))
	require.NoError(t, err)
	read, err := sdkgo.RunQuery(newQuickBooksDexContext("large-read"), client.GetInvoice(), quickbooksConnection, quickbooks.GetInvoiceInput{InvoiceID: testInvoiceID})
	require.NoError(t, err)
	require.Equal(t, quickbooks.GetInvoiceBranchInvalidResponse, read.Branch)
	require.Equal(t, sdkgo.FailureResponseTooLarge, read.Failure.Kind)
	write, err := sdkgo.RunMutation(newQuickBooksDexContext("large-write"), client.SendInvoice(), quickbooksConnection, quickbooks.SendInvoiceInput{InvoiceID: testInvoiceID})
	require.NoError(t, err)
	require.Equal(t, quickbooks.SendInvoiceBranchUncertain, write.Branch)
}
