// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	quickbooks "github.com/superdurable/dex-connectors-library/connectors/intuit/quickbooks/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

var cityAgencyCustomerInput = quickbooks.CreateCustomerInput{DisplayName: "City Agency", EmailAddress: "accounts@cityagency.example.com"}

func cityAgencyCustomerBody() map[string]any {
	return map[string]any{"Customer": customerJSON(testCustomerID, "City Agency", "accounts@cityagency.example.com", true)}
}

func createCityAgency(t *testing.T, ctx *quickbooksDexContext, provider *recordingQuickBooks) (sdkgo.MutationResult[quickbooks.CreateCustomerOutput], error) {
	t.Helper()
	return sdkgo.RunMutation(ctx, newQuickBooksClient(t, provider.URL).CreateCustomer(), quickbooksConnection, cityAgencyCustomerInput)
}

func recordedQuickBooksMarker(t *testing.T, ctx *quickbooksDexContext) singledispatch.Marker {
	t.Helper()
	var marker singledispatch.Marker
	require.NoError(t, json.Unmarshal(ctx.recordedHeartbeat, &marker))
	return marker
}

func requireQuickBooksRetry(t *testing.T, err error) *sdkgo.RetryError {
	t.Helper()
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	return retry
}

func TestUnknownWriteOutcomeIsResentWithTheSameRequestIDBelowTheCap(t *testing.T) {
	for _, status := range []int{http.StatusInternalServerError, http.StatusConflict, http.StatusNotImplemented, http.StatusTooEarly} {
		provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
			if index == 0 {
				writeJSON(t, response, status, `{}`)
				return
			}
			writeValue(t, response, http.StatusOK, cityAgencyCustomerBody())
		})
		first := newQuickBooksDexContext("customer-unknown")
		_, err := createCityAgency(t, first, provider)
		requireQuickBooksRetry(t, err)
		require.Equal(t, singledispatch.Marker{CallID: recordedQuickBooksMarker(t, first).CallID, Attempt: 1, State: singledispatch.StateClaimed, SendCount: 1},
			recordedQuickBooksMarker(t, first), "HTTP %d keeps the claim, so the next attempt counts the send", status)
		result, err := createCityAgency(t, first.nextAttempt(), provider)
		require.NoError(t, err)
		require.Equal(t, quickbooks.CreateCustomerBranchCreated, result.Branch)
		require.Equal(t, provider.request(0).query.Get("requestid"), provider.request(1).query.Get("requestid"))
	}
}

func TestUnknownWriteOutcomeSelectsUncertainOnlyAtTheCap(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusInternalServerError, `{}`)
	})
	ctx := newQuickBooksDexContext("customer-cap")
	for attempt := int32(1); attempt < quickbooks.CreateCustomerDefinition.MaxSends; attempt++ {
		_, err := createCityAgency(t, ctx, provider)
		requireQuickBooksRetry(t, err)
		ctx = ctx.nextAttempt()
	}
	result, err := createCityAgency(t, ctx, provider)
	require.NoError(t, err)
	require.Equal(t, quickbooks.CreateCustomerBranchUncertain, result.Branch)
	require.Equal(t, int(quickbooks.CreateCustomerDefinition.MaxSends), provider.requestCount())

	atCap := ctx.nextAttempt()
	result, err = createCityAgency(t, atCap, provider)
	require.NoError(t, err)
	require.Equal(t, quickbooks.CreateCustomerBranchUncertain, result.Branch, "an attempt at the cap never sends")
	require.Equal(t, int(quickbooks.CreateCustomerDefinition.MaxSends), provider.requestCount())
}

func TestAcceptedUnusableWriteIsReadBackByItsSavedIDAndNeverResent(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, request recordedRequest, index int) {
		switch index {
		case 0:
			writeJSON(t, response, http.StatusOK, `{"Customer":{"Id":"58"}}`)
		case 1:
			writeJSON(t, response, http.StatusServiceUnavailable, `{}`)
		default:
			require.Equal(t, http.MethodGet, request.method, "a saved ID is read back, never resent")
			writeValue(t, response, http.StatusOK, cityAgencyCustomerBody())
		}
	})
	first := newQuickBooksDexContext("customer-saved-id")
	_, err := createCityAgency(t, first, provider)
	requireQuickBooksRetry(t, err)
	require.Equal(t, testCustomerID, recordedQuickBooksMarker(t, first).ProviderID)
	result, err := createCityAgency(t, first.nextAttempt(), provider)
	require.NoError(t, err)
	require.Equal(t, quickbooks.CreateCustomerBranchCreated, result.Branch)
	require.Equal(t, testCustomerID, result.Receipt.ProviderObjectID)
	require.Equal(t, 3, provider.requestCount())
}

func TestAcceptedWriteWithoutAnIDIsNeverResentBelowTheCap(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(http.ResponseWriter)
	}{
		{name: "unusable body", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusOK, `{"Customer":{}}`)
		}},
		{name: "body broken after 2xx headers", reply: func(response http.ResponseWriter) {
			response.Header().Set("Content-Type", "application/json")
			response.Header().Set("Content-Length", "4096")
			response.WriteHeader(http.StatusOK)
			_, _ = response.Write([]byte(`{"Customer":{"Id":"5`)) // The declared length is never reached.
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) { test.reply(response) })
			first := newQuickBooksDexContext("customer-accepted-" + test.name)
			result, err := createCityAgency(t, first, provider)
			require.NoError(t, err)
			require.Equal(t, quickbooks.CreateCustomerBranchUncertain, result.Branch, "QuickBooks accepted the write, so it is never resent")
			require.Equal(t, 1, provider.requestCount())
		})
	}
}

func TestDefiniteRefusalOfAFirstWriteRecordsCanResendAndIsNotResent(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeJSON(t, response, http.StatusBadRequest, faultJSON("ValidationFault", "6240"))
	})
	ctx := newQuickBooksDexContext("customer-name-conflict")
	result, err := createCityAgency(t, ctx, provider)
	require.NoError(t, err)
	require.Equal(t, quickbooks.CreateCustomerBranchNameConflict, result.Branch)
	require.Equal(t, singledispatch.StateCanResend, recordedQuickBooksMarker(t, ctx).State)
	require.Equal(t, 1, provider.requestCount())
}

func TestRefusalAfterAPossibleEarlierWriteSelectsUncertain(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		if index == 0 {
			writeJSON(t, response, http.StatusBadGateway, `{}`)
			return
		}
		writeJSON(t, response, http.StatusBadRequest, faultJSON("ValidationFault", "6240"))
	})
	first := newQuickBooksDexContext("customer-refused-resend")
	_, err := createCityAgency(t, first, provider)
	requireQuickBooksRetry(t, err)
	result, err := createCityAgency(t, first.nextAttempt(), provider)
	require.NoError(t, err)
	require.Equal(t, quickbooks.CreateCustomerBranchUncertain, result.Branch, "the earlier send may have created the customer that now conflicts")
}

func TestUnprocessedWriteIsRetriedWithoutCountingIt(t *testing.T) {
	for _, status := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
			if index == 0 {
				response.Header().Set("Retry-After", "1")
				writeJSON(t, response, status, `{}`)
				return
			}
			writeValue(t, response, http.StatusOK, cityAgencyCustomerBody())
		})
		first := newQuickBooksDexContext("customer-unprocessed")
		_, err := createCityAgency(t, first, provider)
		requireQuickBooksRetry(t, err)
		marker := recordedQuickBooksMarker(t, first)
		require.Equal(t, singledispatch.StateCanResend, marker.State, "HTTP %d", status)
		require.Zero(t, marker.SendCount)
		second := first.nextAttempt()
		result, err := createCityAgency(t, second, provider)
		require.NoError(t, err)
		require.Equal(t, quickbooks.CreateCustomerBranchCreated, result.Branch)
		require.Equal(t, int32(1), recordedQuickBooksMarker(t, second).SendCount)
	}
}

func TestRefusalStatusWithAnIncompleteErrorBodyIsAnUnknownOutcome(t *testing.T) {
	for _, test := range []struct {
		name  string
		reply func(http.ResponseWriter)
	}{
		{name: "oversized 400", reply: func(response http.ResponseWriter) {
			writeJSON(t, response, http.StatusBadRequest, faultJSON("ValidationFault", "6240")+strings.Repeat(" ", 70_000))
		}},
		{name: "404 body broken mid-read", reply: func(response http.ResponseWriter) {
			response.Header().Set("Content-Type", "application/json")
			response.Header().Set("Content-Length", "4096")
			response.WriteHeader(http.StatusNotFound)
			_, _ = response.Write([]byte(`{"Fault":`)) // The declared length is never reached.
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingQuickBooks(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
				if index == 0 {
					test.reply(response)
					return
				}
				writeValue(t, response, http.StatusOK, cityAgencyCustomerBody())
			})
			first := newQuickBooksDexContext("customer-incomplete-refusal-" + test.name)
			_, err := createCityAgency(t, first, provider)
			requireQuickBooksRetry(t, err)
			marker := recordedQuickBooksMarker(t, first)
			require.Equal(t, singledispatch.StateClaimed, marker.State, "an unread refusal body proves nothing, so the send counts")
			require.Equal(t, int32(1), marker.SendCount)
			result, err := createCityAgency(t, first.nextAttempt(), provider)
			require.NoError(t, err)
			require.Equal(t, quickbooks.CreateCustomerBranchCreated, result.Branch)
			require.Equal(t, 2, provider.requestCount())
		})
	}
}

func TestTransientCredentialFailuresRetryWithoutCountingASend(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request is expected") })
	for name, resolveErr := range map[string]error{
		"broker unavailable": projectconfig.ErrCredentialBrokerUnavailable,
		"not configured":     errors.New("connection is not configured"),
		"deadline":           context.DeadlineExceeded,
		"unlisted":           errors.New("unlisted resolve failure"),
	} {
		client, err := quickbooks.New(quickbooks.Config{}, failingResolveCredentials{resolveErr}, quickbooks.WithLocalProviderURL(provider.URL))
		require.NoError(t, err)
		ctx := newQuickBooksDexContext("customer-transient-" + name)
		_, err = sdkgo.RunMutation(ctx, client.CreateCustomer(), quickbooksConnection, cityAgencyCustomerInput)
		retry := requireQuickBooksRetry(t, err)
		require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind, "%s is never a defect", name)
		marker := recordedQuickBooksMarker(t, ctx)
		require.Equal(t, singledispatch.StateCanResend, marker.State)
		require.Zero(t, marker.SendCount)
	}
}

func TestTerminalCredentialFailuresSelectABranchBeforeAnySend(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request is expected") })
	for name, resolveErr := range map[string]error{
		"sdk reauthorization":     sdkgo.NewReauthorizationRequiredError(nil),
		"project reauthorization": projectconfig.ErrReauthorizationRequired,
		"object not found":        projectconfig.ErrObjectNotFound,
		"not selected":            projectconfig.ErrConnectionNotSelected,
		"broker rejected":         projectconfig.ErrCredentialBrokerRejected,
	} {
		client, err := quickbooks.New(quickbooks.Config{}, failingResolveCredentials{resolveErr}, quickbooks.WithLocalProviderURL(provider.URL))
		require.NoError(t, err)
		result, err := sdkgo.RunMutation(newQuickBooksDexContext("customer-terminal-"+name), client.CreateCustomer(), quickbooksConnection, cityAgencyCustomerInput)
		require.NoError(t, err, name)
		require.NotEqual(t, quickbooks.CreateCustomerBranchUncertain, result.Branch, name)
		require.NotEqual(t, quickbooks.CreateCustomerBranchCreated, result.Branch, name)
	}
}

// failingResolveCredentials fails every Resolve with resolveErr.
type failingResolveCredentials struct{ resolveErr error }

func (credentials failingResolveCredentials) Resolve(sdkgo.Call) (quickbooks.Credentials, error) {
	return quickbooks.Credentials{}, credentials.resolveErr
}

func (credentials failingResolveCredentials) ResolveWithRefresh(_ context.Context, call sdkgo.Call,
	_ sdkgo.CredentialRefreshDriver[quickbooks.Credentials]) (quickbooks.Credentials, error) {
	return credentials.Resolve(call)
}

func TestUnreadableWriteCheckpointIsRetriedWithoutARequest(t *testing.T) {
	provider := newRecordingQuickBooks(t, func(http.ResponseWriter, recordedRequest, int) { t.Fatal("no request is expected") })
	first := newQuickBooksDexContext("customer-unreadable")
	first.recordedHeartbeat = json.RawMessage(`"not a checkpoint"`)
	_, err := createCityAgency(t, first.nextAttempt(), provider)
	retry := requireQuickBooksRetry(t, err)
	require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
}
