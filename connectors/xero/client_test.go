// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/xero"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const connectionsList = `[
	{"id":"c1","tenantId":"E0DA6937-DE07-4A14-ADEE-37ABFAC298CE","tenantType":"ORGANISATION","tenantName":"Adam Demo Company (NZ)"},
	{"id":"c2","tenantId":"` + testTenantID + `","tenantType":"ORGANISATION","tenantName":"Maple Florist"},
	{"id":"c3","tenantId":"c3d5e782-2153-4cda-bdb4-cec791ceb90d","tenantType":"PRACTICEMANAGER","tenantName":null}]`

func TestOAuthConnectionDerivesTheNamedOrganisationOnceAndSendsItsTenant(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, request recordedRequest, _ int) {
		switch request.path {
		case "/connections":
			writeJSON(t, response, http.StatusOK, connectionsList)
		default:
			writeValue(t, response, http.StatusOK, map[string]any{"Invoices": []any{invoiceJSON()}})
		}
	})
	client := newOAuthClient(t, provider.URL, " maple florist ")
	for range 2 {
		result, err := sdkgo.RunQuery(newXeroDexContext("get"), client.GetInvoice(), xeroConnection, xero.GetInvoiceInput{InvoiceID: testInvoiceID})
		require.NoError(t, err)
		require.Equal(t, xero.GetInvoiceBranchFound, result.Branch)
		require.Equal(t, testTenantID, result.Receipt.Metadata[xero.TenantIDReceiptKey])
	}
	require.Len(t, provider.requestsTo("/connections"), 1, "the derived tenant is remembered")
	require.Equal(t, "Bearer "+testAccessToken, provider.requestsTo("/connections")[0].header.Get("Authorization"))
	for _, request := range provider.requestsTo("/api.xro/2.0/Invoices/" + testInvoiceID) {
		require.Equal(t, testTenantID, request.header.Get("Xero-Tenant-Id"))
	}
}

func TestOAuthConnectionTakesATenantIDWithoutReadingConnections(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeValue(t, response, http.StatusOK, map[string]any{"Invoices": []any{invoiceJSON()}})
	})
	client := newOAuthClient(t, provider.URL, strings.ToUpper(testTenantID))
	result, err := sdkgo.RunQuery(newXeroDexContext("get"), client.GetInvoice(), xeroConnection, xero.GetInvoiceInput{InvoiceID: testInvoiceID})
	require.NoError(t, err)
	require.Equal(t, xero.GetInvoiceBranchFound, result.Branch)
	require.Empty(t, provider.requestsTo("/connections"))
	require.Equal(t, testTenantID, provider.request(0).header.Get("Xero-Tenant-Id"))
}

func TestOAuthOrganisationSelectionFailsClosedWithoutEchoingNames(t *testing.T) {
	for _, test := range []struct {
		name         string
		organisation string
		connections  string
	}{
		{name: "several organisations and a blank name", connections: connectionsList},
		{name: "no organisation connected", connections: `[{"tenantId":"c3d5e782-2153-4cda-bdb4-cec791ceb90d","tenantType":"PRACTICEMANAGER"}]`},
		{name: "unknown name", organisation: "Unknown Florist", connections: connectionsList},
		{name: "duplicate name", organisation: "Maple Florist", connections: `[
			{"tenantId":"` + testTenantID + `","tenantType":"ORGANISATION","tenantName":"Maple Florist"},
			{"tenantId":"e0da6937-de07-4a14-adee-37abfac298ce","tenantType":"ORGANISATION","tenantName":"maple florist"}]`},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
				writeJSON(t, response, http.StatusOK, test.connections)
			})
			result, err := sdkgo.RunQuery(newXeroDexContext("select"), newOAuthClient(t, provider.URL, test.organisation).GetInvoice(), xeroConnection,
				xero.GetInvoiceInput{InvoiceID: testInvoiceID})
			require.NoError(t, err)
			require.Equal(t, xero.GetInvoiceBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, "Maple")
			require.NotContains(t, result.Failure.Message, "Adam")
			require.Equal(t, 1, provider.requestCount(), "no Accounting API request is sent without a tenant")
		})
	}
}

func TestForbiddenTenantIsReadAgainOnTheNextCall(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, request recordedRequest, index int) {
		switch {
		case request.path == "/connections":
			writeJSON(t, response, http.StatusOK, `[{"tenantId":"`+testTenantID+`","tenantType":"ORGANISATION","tenantName":"Maple Florist"}]`)
		case index == 1:
			writeJSON(t, response, http.StatusForbidden, `{"title":"Forbidden","status":403,"detail":"AuthenticationUnsuccessful"}`)
		default:
			writeValue(t, response, http.StatusOK, map[string]any{"Invoices": []any{invoiceJSON()}})
		}
	})
	client := newOAuthClient(t, provider.URL, "")
	result, err := sdkgo.RunQuery(newXeroDexContext("forbidden"), client.GetInvoice(), xeroConnection, xero.GetInvoiceInput{InvoiceID: testInvoiceID})
	require.NoError(t, err)
	require.Equal(t, xero.GetInvoiceBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, result.Failure.Kind)
	result, err = sdkgo.RunQuery(newXeroDexContext("again"), client.GetInvoice(), xeroConnection, xero.GetInvoiceInput{InvoiceID: testInvoiceID})
	require.NoError(t, err)
	require.Equal(t, xero.GetInvoiceBranchFound, result.Branch)
	require.Len(t, provider.requestsTo("/connections"), 2)
}

func TestReadStatusesMapToBranchesWithoutXeroText(t *testing.T) {
	for _, test := range []struct {
		name   string
		reply  func(http.ResponseWriter)
		branch sdkgo.BranchID
		kind   sdkgo.FailureKind
		retry  bool
	}{
		{name: "revoked token", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusUnauthorized, `{"Type":null,"Title":"Unauthorized","Status":401,"Detail":"AuthenticationUnsuccessful"}`)
		}, branch: xero.GetInvoiceBranchProviderRejected, kind: sdkgo.FailureAuthentication},
		{name: "missing invoice", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusNotFound, `{"Title":"SENTINEL"}`)
		}, branch: xero.GetInvoiceBranchNotFound, kind: sdkgo.FailureNotFound},
		{name: "redirect", reply: func(response http.ResponseWriter) {
			response.Header().Set("Location", "https://elsewhere.example.com/")
			writeJSON(t, response, http.StatusFound, `{}`)
		}, branch: xero.GetInvoiceBranchProviderRejected, kind: sdkgo.FailureProtocol},
		{name: "precondition", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusPreconditionFailed, `SENTINEL`)
		}, branch: xero.GetInvoiceBranchProviderRejected, kind: sdkgo.FailureProviderRejection},
		{name: "credential reflected", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"Invoices":[{"Reference":"`+testAccessToken+`"}]}`)
		}, branch: xero.GetInvoiceBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "not JSON", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `<Response/>`)
		}, branch: xero.GetInvoiceBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "exponent amount", reply: func(response http.ResponseWriter) {
			encoded, err := json.Marshal(map[string]any{"Invoices": []any{invoiceJSON()}})
			require.NoError(t, err)
			writeJSON(t, response, http.StatusOK, strings.Replace(string(encoded), `"Total":109.89`, `"Total":1.0989E2`, 1))
		}, branch: xero.GetInvoiceBranchInvalidResponse, kind: sdkgo.FailureProtocol},
		{name: "internal error", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusInternalServerError, `{}`)
		}, kind: sdkgo.FailureAvailability, retry: true},
		{name: "organisation offline", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusServiceUnavailable, `The Organisation is offline`)
		}, kind: sdkgo.FailureAvailability, retry: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { test.reply(response) })
			result, err := sdkgo.RunQuery(newXeroDexContext("status-"+test.name), newXeroClient(t, provider.URL).GetInvoice(), xeroConnection,
				xero.GetInvoiceInput{InvoiceID: testInvoiceID})
			if test.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, test.kind, retry.Failure.Kind)
				return
			}
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.kind != "" {
				require.Equal(t, test.kind, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
				require.NotContains(t, result.Failure.Message, testAccessToken)
			}
		})
	}
}

func TestDailyLimitOnAReadSelectsItsBranchWithTheResetDelay(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		response.Header().Set("X-Rate-Limit-Problem", "Day")
		response.Header().Set("Retry-After", "86000")
		response.Header().Set("X-DayLimit-Remaining", "0")
		writeJSON(t, response, http.StatusTooManyRequests, `{}`)
	})
	result, err := sdkgo.RunQuery(newXeroDexContext("daily"), newXeroClient(t, provider.URL).ListInvoices(), xeroConnection, xero.ListInvoicesInput{})
	require.NoError(t, err)
	require.Equal(t, xero.ListInvoicesBranchDailyLimitReached, result.Branch)
	require.Equal(t, sdkgo.FailureQuotaExhausted, result.Failure.Kind)
	require.Equal(t, map[string]string{
		xero.RetryAfterSecondsReceiptKey: "86000", xero.RateLimitProblemReceiptKey: "day", xero.DayLimitRemainingReceiptKey: "0",
	}, result.Receipt.Metadata, "the full reset delay is kept, not the one-hour retry cap")
}

func TestRemainingCallCountsReachTheReceipt(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		response.Header().Set("X-DayLimit-Remaining", "4990")
		response.Header().Set("X-MinLimit-Remaining", "59")
		response.Header().Set("X-AppMinLimit-Remaining", "not-a-count")
		writeValue(t, response, http.StatusOK, map[string]any{"Invoices": []any{}})
	})
	result, err := sdkgo.RunQuery(newXeroDexContext("counts"), newXeroClient(t, provider.URL).ListInvoices(), xeroConnection, xero.ListInvoicesInput{})
	require.NoError(t, err)
	require.Equal(t, map[string]string{xero.DayLimitRemainingReceiptKey: "4990", xero.MinuteLimitRemainingReceiptKey: "59"}, result.Receipt.Metadata)
}

func TestMinuteLimitRetriesAfterXerosDelay(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		response.Header().Set("X-Rate-Limit-Problem", "minute")
		response.Header().Set("Retry-After", "42")
		writeJSON(t, response, http.StatusTooManyRequests, `{}`)
	})
	_, err := sdkgo.RunQuery(newXeroDexContext("minute"), newXeroClient(t, provider.URL).ListContacts(), xeroConnection, xero.ListContactsInput{})
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 42*time.Second, retryAfter.After)
}

func TestNewValidatesConfigurationAndTheLocalProviderURL(t *testing.T) {
	credentials := customConnectionCredentials()
	_, err := xero.New(xero.Config{}, nil)
	require.Error(t, err)
	_, err = xero.New(xero.Config{MaxResponseBytes: -1}, credentials)
	require.Error(t, err)
	_, err = xero.New(xero.Config{Organisation: "Maple\nFlorist"}, credentials)
	require.Error(t, err)
	_, err = xero.New(xero.Config{}, credentials, nil)
	require.Error(t, err)
	for _, providerURL := range []string{"https://api.xero.com", "http://192.0.2.10:8080", "http://127.0.0.1:8080/api.xro/2.0", "ftp://127.0.0.1"} {
		_, err = xero.New(xero.Config{}, credentials, xero.WithLocalProviderURL(providerURL))
		require.Error(t, err, providerURL)
	}
	client, err := xero.New(xero.Config{}, credentials, xero.WithLocalProviderURL("http://localhost:8080"))
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestTransportFailuresRetryAndNeverReachAnotherHost(t *testing.T) {
	provider := newRecordingXero(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { dropConnection(t, response) })
	_, err := sdkgo.RunQuery(newXeroDexContext("dropped"), newXeroClient(t, provider.URL).GetInvoice(), xeroConnection, xero.GetInvoiceInput{InvoiceID: testInvoiceID})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)

	refused := newRecordingXero(t, func(http.ResponseWriter, recordedRequest, int) {})
	refusedURL := refused.URL
	refused.Close()
	_, err = sdkgo.RunQuery(newXeroDexContext("refused"), newXeroClient(t, refusedURL).GetInvoice(), xeroConnection, xero.GetInvoiceInput{InvoiceID: testInvoiceID})
	require.ErrorAs(t, err, &retry)
	require.Contains(t, retry.Failure.Message, "no request was sent")
}
