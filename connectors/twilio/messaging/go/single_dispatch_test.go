// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestSendMessageFenceAllowsOneRetryAfterProviderRateLimitOrUnavailability(t *testing.T) {
	for _, statusCode := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			var requests int
			provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
				requests++
				if requests == 1 {
					response.WriteHeader(statusCode)
					return
				}
				response.WriteHeader(http.StatusCreated)
				writeBody(response, messageJSON(testMessageSID, "queued", "null"))
			})
			client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
			ctx := singledispatchtest.NewContext("twilio-fence-retry", "send-message")
			_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			ctx.AdvanceAttempt()
			marker := readPersistedMarker(t, ctx)
			require.Equal(t, singledispatch.StateCanResend, marker.State)
			result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
			require.NoError(t, err)
			require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
			require.Equal(t, testMessageSID, result.Value.SID)
			require.Equal(t, marker.CallID, result.Receipt.CallID)
			require.Len(t, provider.recordedRequests(), 2)
		})
	}
}

func TestSendMessageFenceCountsEveryAttemptWhenTheClaimIsLost(t *testing.T) {
	var requests int
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("reply was lost")
	})
	client := newTwilioClient(t, "https://api.twilio.test/2010-04-01", messaging.Config{DefaultSender: testSender}, authTokenCredentials(), messaging.WithHTTPClient(&http.Client{Transport: transport}))
	ctx := singledispatchtest.NewContext("twilio-fence-lost-claim", "send-message")
	ctx.LoseNextHeartbeat()
	for attempt := 1; attempt < 3; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, "attempt %d is below the send cap", attempt)
		ctx.AdvanceAttempt()
	}
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
	require.Equal(t, 3, requests)
}

func TestSendMessageFenceResendsAnUnknownOutcomeOnlyBelowTheSendCap(t *testing.T) {
	var posts atomic.Int32
	provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
		if posts.Add(1) < 3 {
			response.WriteHeader(http.StatusBadGateway)
			return
		}
		response.WriteHeader(http.StatusCreated)
		writeBody(response, messageJSON(testMessageSID, "queued", "null"))
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	ctx := singledispatchtest.NewContext("twilio-fence-resend", "send-message")
	for attempt := 1; attempt < 3; attempt++ {
		_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		ctx.AdvanceAttempt()
	}
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
	ctx.AdvanceAttempt()
	marker := readPersistedMarker(t, ctx)
	require.EqualValues(t, 3, marker.SendCount)
	require.Len(t, provider.recordedRequests(), 3)
}

func TestSendMessageFenceNeverResendsADefiniteRefusal(t *testing.T) {
	for _, statusCode := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusRequestEntityTooLarge, http.StatusUnprocessableEntity} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(statusCode)
				writeBody(response, errorJSON(statusCode, 21211))
			})
			client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
			ctx := singledispatchtest.NewContext("twilio-fence-refusal", "send-message")
			result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
			require.NoError(t, err)
			require.Equal(t, messaging.SendMessageBranchProviderRejected, result.Branch)
			ctx.AdvanceAttempt()
			marker := readPersistedMarker(t, ctx)
			require.Equal(t, singledispatch.StateCanResend, marker.State)
			require.Len(t, provider.recordedRequests(), 1)
		})
	}
}

func TestSendMessageFenceSelectsUncertainForARefusalOfAResend(t *testing.T) {
	var posts atomic.Int32
	provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
		if posts.Add(1) == 1 {
			response.WriteHeader(http.StatusInternalServerError)
			return
		}
		response.WriteHeader(http.StatusBadRequest)
		writeBody(response, errorJSON(http.StatusBadRequest, 21211))
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	ctx := singledispatchtest.NewContext("twilio-fence-refused-resend", "send-message")
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
	require.Len(t, provider.recordedRequests(), 2)
}

func TestSendMessageFenceSelectsUncertainForTerminalCredentialsAfterAPossibleSend(t *testing.T) {
	provider := newTwilioFake(t, respondWithStatus(http.StatusInternalServerError))
	credentials := &switchingTwilioCredentials{credentials: messaging.Credentials{AuthMethodID: messaging.AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString(testAuthToken)}}
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, credentials)
	ctx := singledispatchtest.NewContext("twilio-fence-local-after-send", "send-message")
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	credentials.setError(sdkgo.NewReauthorizationRequiredError(errors.New("revoked")))
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch, "defect would hide the earlier possible send")
	require.Len(t, provider.recordedRequests(), 1)
}

func TestSendMessageFenceReadsBackAClientErrorThatNamesACreatedMessage(t *testing.T) {
	provider := newTwilioFake(t, func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			writeBody(response, messageJSON(testMessageSID, "queued", "null"))
			return
		}
		response.WriteHeader(http.StatusConflict)
		writeBody(response, messageJSON(testMessageSID, "queued", "null"))
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	ctx := singledispatchtest.NewContext("twilio-fence-error-with-sid", "send-message")
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
	require.Equal(t, testMessageSID, result.Value.SID)
	requests := provider.recordedRequests()
	require.Len(t, requests, 2)
	require.Equal(t, http.MethodGet, requests[1].method)
}

func TestSendMessageFenceSelectsUncertainWhenANamedSIDIsUnsavedAndItsReadBackFails(t *testing.T) {
	ctx := singledispatchtest.NewContext("twilio-fence-error-with-unsaved-sid", "send-message")
	provider := newTwilioFake(t, func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			response.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		// The claim is already saved; the provider ID checkpoint that follows fails.
		ctx.SetWriteError(errors.New("heartbeat unavailable"))
		response.WriteHeader(http.StatusConflict)
		writeBody(response, messageJSON(testMessageSID, "queued", "null"))
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err, "a Retry without the saved SID could resend below the cap")
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
	require.Len(t, provider.recordedRequests(), 2)
}

func TestSendMessageFenceRetriesTheReadBackOfASavedNamedSIDWithoutResending(t *testing.T) {
	reads := 0
	provider := newTwilioFake(t, func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			reads++
			if reads == 1 {
				response.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			writeBody(response, messageJSON(testMessageSID, "queued", "null"))
			return
		}
		response.WriteHeader(http.StatusConflict)
		writeBody(response, messageJSON(testMessageSID, "queued", "null"))
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	ctx := singledispatchtest.NewContext("twilio-fence-error-with-saved-sid", "send-message")
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
	requests := provider.recordedRequests()
	require.Len(t, requests, 3)
	require.Equal(t, http.MethodGet, requests[2].method, "the saved SID is read back, never resent")
}

func TestSendMessageFenceRecordsCredentialFailureBeforeRetry(t *testing.T) {
	provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusCreated)
		writeBody(response, messageJSON(testMessageSID, "queued", "null"))
	})
	credentials := &recoveringTwilioCredentials{credentials: messaging.Credentials{AuthMethodID: messaging.AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString(testAuthToken)}}
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, credentials)
	ctx := singledispatchtest.NewContext("twilio-fence-credentials", "send-message")
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Empty(t, provider.recordedRequests())
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
	require.Len(t, provider.recordedRequests(), 1)
}

func TestSendMessageFenceClassifiesCredentialLookupBeforeSend(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		lookupErr   error
		credentials messaging.Credentials
		isTerminal  bool
	}{
		{"reauthorization", sdkgo.NewReauthorizationRequiredError(errors.New("revoked")), messaging.Credentials{}, true},
		{"project reauthorization", projectconfig.ErrReauthorizationRequired, messaging.Credentials{}, true},
		{"object not found", projectconfig.ErrObjectNotFound, messaging.Credentials{}, true},
		{"connection not selected", projectconfig.ErrConnectionNotSelected, messaging.Credentials{}, true},
		{"broker rejected", projectconfig.ErrCredentialBrokerRejected, messaging.Credentials{}, true},
		{"broker unavailable", projectconfig.ErrCredentialBrokerUnavailable, messaging.Credentials{}, false},
		{"static not configured", errors.New("connection is not configured"), messaging.Credentials{}, false},
		{"store unavailable", errors.New("project connection store returned HTTP 503"), messaging.Credentials{}, false},
		{"connection refused", errors.New("connect: connection refused"), messaging.Credentials{}, false},
		{"invalid credentials", nil, messaging.Credentials{AuthMethodID: messaging.AuthTokenAuthMethodID}, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newTwilioFake(t, respondWithStatus(http.StatusCreated))
			credentials := &switchingTwilioCredentials{lookupErr: testCase.lookupErr, credentials: testCase.credentials}
			client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, credentials)
			ctx := singledispatchtest.NewContext("twilio-credential-"+testCase.name, "send-message")
			result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, messaging.SendMessageBranchDefect, result.Branch)
				require.Contains(t, result.Failure.Message, "reconnect")
			} else {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
			}
			require.Empty(t, provider.recordedRequests())
			ctx.AdvanceAttempt()
			marker := readPersistedMarker(t, ctx)
			require.Equal(t, singledispatch.StateNotSent, marker.State)
		})
	}
}

func TestSendMessageFenceClassifiesCredentialLookupDuringReadBack(t *testing.T) {
	for _, testCase := range []struct {
		name               string
		lookupErr          error
		invalidCredentials bool
		isTerminal         bool
	}{
		{"reauthorization", sdkgo.NewReauthorizationRequiredError(errors.New("revoked")), false, true},
		{"broker rejected", projectconfig.ErrCredentialBrokerRejected, false, true},
		{"connection not selected", projectconfig.ErrConnectionNotSelected, false, true},
		{"broker unavailable", projectconfig.ErrCredentialBrokerUnavailable, false, false},
		{"static not configured", errors.New("connection is not configured"), false, false},
		{"store unavailable", errors.New("project connection store returned HTTP 503"), false, false},
		{"invalid credentials", nil, true, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(http.StatusCreated)
				writeBody(response, messageJSON(testMessageSID, "queued", "null"))
			})
			credentials := &switchingTwilioCredentials{credentials: messaging.Credentials{AuthMethodID: messaging.AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString(testAuthToken)}}
			client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, credentials)
			ctx := singledispatchtest.NewContext("twilio-read-credential-"+testCase.name, "send-message")
			result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
			require.NoError(t, err)
			require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
			credentials.setError(testCase.lookupErr)
			if testCase.invalidCredentials {
				credentials.setCredentials(messaging.Credentials{AuthMethodID: messaging.AuthTokenAuthMethodID})
			}
			ctx.AdvanceAttempt()
			result, err = sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
			if testCase.isTerminal {
				require.NoError(t, err)
				require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
				require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
				require.Contains(t, result.Failure.Message, "reconnect")
				require.Equal(t, testMessageSID, result.Receipt.ProviderObjectID)
			} else {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
			}
			require.Len(t, provider.recordedRequests(), 1)
		})
	}
}

func TestSendMessageFenceRetriesAfterCredentialRotation(t *testing.T) {
	provider := newTwilioFake(t, func(response http.ResponseWriter, request *http.Request) {
		_, password, _ := request.BasicAuth()
		if password == testAuthToken {
			response.WriteHeader(http.StatusUnauthorized)
			return
		}
		response.WriteHeader(http.StatusCreated)
		writeBody(response, messageJSON(testMessageSID, "queued", "null"))
	})
	credentials := &rotatingTwilioCredentials{}
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, credentials)
	ctx := singledispatchtest.NewContext("twilio-fence-rotated", "send-message")
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
	require.Len(t, provider.recordedRequests(), 2)
}

func TestSendMessageFenceClassifiesRejectedCredentialRefresh(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		err     error
		message string
	}{
		{"reauthorization", sdkgo.NewReauthorizationRequiredError(errors.New("revoked")), "reconnect"},
		{"rejected credential not refreshed", fmt.Errorf("forced read: %w", projectconfig.ErrRejectedCredentialNotRefreshed), "update the connection"},
		{"expiry not proved", errors.New("authentication rejection does not prove credential expiry"), "update the connection"},
		{"refresh unavailable", errors.New("credential store unavailable"), "temporarily unavailable"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(http.StatusUnauthorized)
				writeBody(response, `{"code":20003}`)
			})
			credentials := &failingRefreshTwilioCredentials{refreshErr: testCase.err}
			client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, credentials)
			ctx := singledispatchtest.NewContext("twilio-rejected-refresh-"+testCase.name, "send-message")
			result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
			require.NoError(t, err)
			require.Equal(t, messaging.SendMessageBranchProviderRejected, result.Branch)
			require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
			require.Equal(t, 20003, result.Value.ErrorCode)
			require.Equal(t, "20003", result.Receipt.Metadata["twilioErrorCode"])
			require.Contains(t, result.Failure.Message, testCase.message)
			if testCase.message == "update the connection" {
				require.NotContains(t, result.Failure.Message, "temporarily unavailable")
			}
			require.Len(t, provider.recordedRequests(), 1)
		})
	}
}

func TestSendMessageFenceKeepsReauthorizationFailureTerminal(t *testing.T) {
	provider := newTwilioFake(t, respondWithStatus(http.StatusCreated))
	credentials := &reauthorizationRequiredTwilioCredentials{}
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, credentials)
	ctx := singledispatchtest.NewContext("twilio-fence-reauth", "send-message")
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchDefect, result.Branch)
	require.Empty(t, provider.recordedRequests())
}

func TestSendMessageFenceRetriesFailedClaimWithoutSending(t *testing.T) {
	provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusCreated)
		writeBody(response, messageJSON(testMessageSID, "queued", "null"))
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	ctx := singledispatchtest.NewContext("twilio-fence-claim-failed", "send-message")
	ctx.SetWriteError(errors.New("heartbeat unavailable"))
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Empty(t, provider.recordedRequests())
	ctx.SetWriteError(nil)
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err, "the lost checkpoint counts as one possible send, which stays below the cap")
	require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
	require.Len(t, provider.recordedRequests(), 1)
}

func TestSendMessageFenceReadsBackAcceptedMessageWithoutResend(t *testing.T) {
	for _, readStatus := range []int{http.StatusOK, http.StatusRequestTimeout, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(readStatus), func(t *testing.T) {
			provider := newTwilioFake(t, func(response http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					response.WriteHeader(readStatus)
					if readStatus == http.StatusOK {
						writeBody(response, messageJSON(testMessageSID, "delivered", "null"))
					}
					return
				}
				response.WriteHeader(http.StatusCreated)
				writeBody(response, messageJSON(testMessageSID, "queued", "null"))
			})
			client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
			ctx := singledispatchtest.NewContext("twilio-fence-accepted", "send-message")
			result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
			require.NoError(t, err)
			require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
			ctx.AdvanceAttempt()
			result, err = sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
			if readStatus == http.StatusOK {
				require.NoError(t, err)
				require.Equal(t, messaging.MessageStatusDelivered, result.Value.Status)
			} else {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
			}
			requests := provider.recordedRequests()
			require.Len(t, requests, 2)
			require.Equal(t, http.MethodPost, requests[0].method)
			require.Equal(t, http.MethodGet, requests[1].method)
		})
	}
}

func TestSendMessageFenceKeepsReadBackInsideStepDeadline(t *testing.T) {
	provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusCreated)
		writeBody(response, messageJSON(testMessageSID, "queued", "null"))
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	ctx := singledispatchtest.NewContext("twilio-fence-read-deadline", "send-message")
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
	ctx.AdvanceAttempt()
	cancel := ctx.SetDeadline(time.Now().Add(500 * time.Millisecond))
	defer cancel()
	_, err = sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Len(t, provider.recordedRequests(), 1)
}

func TestSendMessageFenceRefusesExpiredAttemptBeforeSending(t *testing.T) {
	provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusCreated)
		writeBody(response, messageJSON(testMessageSID, "queued", "null"))
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	ctx := singledispatchtest.NewContext("twilio-fence-deadline", "send-message")
	cancel := ctx.SetDeadline(time.Now().Add(-time.Second))
	defer cancel()
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Empty(t, provider.recordedRequests())
	ctx.AdvanceAttempt()
	reset := ctx.SetDeadline(time.Now().Add(time.Minute))
	defer reset()
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
	require.Len(t, provider.recordedRequests(), 1)
}

func TestSendMessageFenceCutsOffAnInFlightWrite(t *testing.T) {
	provider := newTwilioFake(t, func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	ctx := singledispatchtest.NewContext("twilio-fence-cut", "send-message")
	cancel := ctx.SetDeadline(time.Now().Add(500 * time.Millisecond))
	defer cancel()
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a cut-off write is an unknown outcome below the send cap")
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
	require.Len(t, provider.recordedRequests(), 1)
}

func TestSendMessageFenceNeverResendsUnconfirmedSuccessResponses(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		respond func(http.ResponseWriter, *http.Request)
		kind    sdkgo.FailureKind
	}{
		{"body without SID", func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusCreated)
			writeBody(response, `{"status":"queued"}`)
		}, sdkgo.FailureProtocol},
		{"interrupted body", func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Content-Length", "1000")
			response.WriteHeader(http.StatusCreated)
			writeBody(response, "{")
		}, sdkgo.FailureTransport},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newTwilioFake(t, testCase.respond)
			client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
			ctx := singledispatchtest.NewContext("twilio-fence-unconfirmed", "send-message")
			result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
			require.NoError(t, err, "Twilio created the message, so a resend could duplicate it")
			require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			require.Len(t, provider.recordedRequests(), 1)
		})
	}
}

func TestSendMessageFenceReadsBackAnAcceptedSIDWhoseMessageDoesNotDecode(t *testing.T) {
	provider := newTwilioFake(t, func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodGet {
			writeBody(response, messageJSON(testMessageSID, "queued", "null"))
			return
		}
		response.WriteHeader(http.StatusCreated)
		writeBody(response, messageJSON(testMessageSID, "Not A Status", "null"))
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	ctx := singledispatchtest.NewContext("twilio-fence-accepted-undecodable", "send-message")
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
	require.Equal(t, testMessageSID, result.Value.SID)
	requests := provider.recordedRequests()
	require.Len(t, requests, 2)
	require.Equal(t, http.MethodGet, requests[1].method)
	ctx.AdvanceAttempt()
	require.Equal(t, testMessageSID, readPersistedMarker(t, ctx).ProviderID)
}

func TestSendMessageFenceNeverSavesANamedSIDThatContainsTheCredential(t *testing.T) {
	const hexAuthToken = "0123456789abcdef0123456789abcdef"
	reflectedSID := "SM" + hexAuthToken
	for _, statusCode := range []int{http.StatusCreated, http.StatusConflict} {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(statusCode)
				writeBody(response, messageJSON(reflectedSID, "queued", "null"))
			})
			credentials := staticCredentials(messaging.Credentials{AuthMethodID: messaging.AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString(hexAuthToken)})
			client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, credentials)
			ctx := singledispatchtest.NewContext("twilio-fence-reflected-sid", "send-message")
			result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
			require.NoError(t, err)
			require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
			require.Len(t, provider.recordedRequests(), 1, "a SID that contains the credential is never read back")
			require.NotContains(t, result.Receipt.ProviderObjectID, hexAuthToken)
			ctx.AdvanceAttempt()
			require.Empty(t, readPersistedMarker(t, ctx).ProviderID)
		})
	}
}

func TestSendMessageFenceTreatsAnUnreadableUnauthorizedBodyAsAnUnknownOutcome(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		respond func(http.ResponseWriter)
	}{
		{"oversized body", func(response http.ResponseWriter) {
			response.WriteHeader(http.StatusUnauthorized)
			writeBody(response, `{"code":20003,"message":"`+strings.Repeat("x", 70<<10)+`"}`)
		}},
		{"interrupted body", func(response http.ResponseWriter) {
			response.Header().Set("Content-Length", "1000")
			response.WriteHeader(http.StatusUnauthorized)
			writeBody(response, "{")
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var posts atomic.Int32
			provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
				if posts.Add(1) == 1 {
					testCase.respond(response)
					return
				}
				response.WriteHeader(http.StatusUnauthorized)
				writeBody(response, errorJSON(http.StatusUnauthorized, 20003))
			})
			client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
			ctx := singledispatchtest.NewContext("twilio-fence-unreadable-unauthorized", "send-message")
			_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry, "an unreadable 401 cannot prove that Twilio refused the message")
			ctx.AdvanceAttempt()
			require.Equal(t, singledispatch.StateClaimed, readPersistedMarker(t, ctx).State)
			result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
			require.NoError(t, err)
			require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
			require.Len(t, provider.recordedRequests(), 2)
		})
	}
}

func TestSendMessageFenceLeavesUnconfirmableReadBackUncertain(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		respond func(http.ResponseWriter)
	}{
		{"missing", func(response http.ResponseWriter) { response.WriteHeader(http.StatusNotFound) }},
		{"different SID", func(response http.ResponseWriter) {
			writeBody(response, messageJSON("SM00000000000000000000000000000000", "queued", "null"))
		}},
		{"different recipient", func(response http.ResponseWriter) {
			writeBody(response, strings.ReplaceAll(messageJSON(testMessageSID, "queued", "null"), testRecipient, "+14155550999"))
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newTwilioFake(t, func(response http.ResponseWriter, request *http.Request) {
				if request.Method == http.MethodGet {
					testCase.respond(response)
					return
				}
				response.WriteHeader(http.StatusCreated)
				writeBody(response, messageJSON(testMessageSID, "queued", "null"))
			})
			client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
			ctx := singledispatchtest.NewContext("twilio-read-unconfirmed-"+testCase.name, "send-message")
			result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
			require.NoError(t, err)
			require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
			ctx.AdvanceAttempt()
			result, err = sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
			require.NoError(t, err)
			require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
			require.Equal(t, testMessageSID, result.Receipt.ProviderObjectID)
			requests := provider.recordedRequests()
			require.Len(t, requests, 2)
			require.Equal(t, http.MethodPost, requests[0].method)
			require.Equal(t, http.MethodGet, requests[1].method)
		})
	}
}

func TestSendMessageFenceRetriesRefusedConnectionOnNextAttempt(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL
	closed.Close()
	client := newTwilioClient(t, closedURL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	ctx := singledispatchtest.NewContext("twilio-fence-refused", "send-message")
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusCreated)
		writeBody(response, messageJSON(testMessageSID, "queued", "null"))
	})
	client = newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
	require.Len(t, provider.recordedRequests(), 1)
}

func TestSendMessageFencePreventsTransparentReplayOnReusedConnection(t *testing.T) {
	var posts atomic.Int32
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/prime" {
			response.WriteHeader(http.StatusOK)
			return
		}
		posts.Add(1)
		if _, err := io.Copy(io.Discard, request.Body); err != nil {
			t.Errorf("read request body: %v", err)
		}
		connection, _, err := response.(http.Hijacker).Hijack()
		if err != nil {
			t.Errorf("hijack response: %v", err)
			return
		}
		if err := connection.Close(); err != nil {
			t.Errorf("close response: %v", err)
		}
	}))
	defer provider.Close()
	transport := &idempotencyHeaderTransport{inner: &http.Transport{}}
	defer transport.inner.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport}
	prime, err := httpClient.Get(provider.URL + "/prime")
	require.NoError(t, err)
	require.NoError(t, prime.Body.Close())
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials(), messaging.WithHTTPClient(httpClient))
	ctx := singledispatchtest.NewContext("twilio-fence-reused", "send-message")
	_, err = sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a connection lost after dispatch resends only on a later attempt")
	require.EqualValues(t, 1, posts.Load())
}

func TestSendMessageFenceCountsAMissedAttemptAfterAStaleResendMarker(t *testing.T) {
	provider := newTwilioFake(t, respondWithStatus(http.StatusServiceUnavailable))
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	ctx := singledispatchtest.NewContext("twilio-fence-stale", "send-message")
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	ctx.AdvanceAttempt()
	_, err = sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.ErrorAs(t, err, &retry, "the missed attempt counts as one possible send, which stays below the cap")
	ctx.AdvanceAttempt()
	marker := readPersistedMarker(t, ctx)
	require.EqualValues(t, 1, marker.SendCount)
	require.Len(t, provider.recordedRequests(), 2)
}

func TestSendMessageFenceRetriesAnUnreadableCheckpointWithoutSending(t *testing.T) {
	provider := newTwilioFake(t, respondWithStatus(http.StatusCreated))
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	ctx := singledispatchtest.NewContext("twilio-fence-unreadable", "send-message")
	ctx.AdvanceAttempt()
	ctx.SetReadError(errors.New("heartbeat unavailable"))
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "an untrusted checkpoint never selects a terminal branch")
	require.Empty(t, provider.recordedRequests())
}

type recoveringTwilioCredentials struct {
	mutex       sync.Mutex
	resolutions int
	credentials messaging.Credentials
}

type switchingTwilioCredentials struct {
	mutex       sync.Mutex
	lookupErr   error
	credentials messaging.Credentials
}

func (provider *switchingTwilioCredentials) Resolve(sdkgo.Call) (messaging.Credentials, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return provider.credentials, provider.lookupErr
}

func (provider *switchingTwilioCredentials) setError(lookupErr error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.lookupErr = lookupErr
}

func (provider *switchingTwilioCredentials) setCredentials(credentials messaging.Credentials) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.credentials = credentials
}

type rotatingTwilioCredentials struct {
	mutex       sync.Mutex
	resolutions int
}

type failingRefreshTwilioCredentials struct{ refreshErr error }

func (*failingRefreshTwilioCredentials) Resolve(sdkgo.Call) (messaging.Credentials, error) {
	return messaging.Credentials{AuthMethodID: messaging.AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString(testAuthToken)}, nil
}

func (provider *failingRefreshTwilioCredentials) ResolveAfterRejection(_ context.Context, _ sdkgo.Call, _ sdkgo.CredentialRefreshDriver[messaging.Credentials]) (messaging.Credentials, error) {
	return messaging.Credentials{}, provider.refreshErr
}

func (provider *rotatingTwilioCredentials) Resolve(sdkgo.Call) (messaging.Credentials, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.resolutions++
	secret := testAuthToken
	if provider.resolutions > 1 {
		secret = "rotated-auth-token-sentinel"
	}
	return messaging.Credentials{AuthMethodID: messaging.AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString(secret)}, nil
}

func (provider *rotatingTwilioCredentials) ResolveAfterRejection(_ context.Context, _ sdkgo.Call, _ sdkgo.CredentialRefreshDriver[messaging.Credentials]) (messaging.Credentials, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.resolutions++
	return messaging.Credentials{AuthMethodID: messaging.AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString("rotated-auth-token-sentinel")}, nil
}

type reauthorizationRequiredTwilioCredentials struct{}

func (*reauthorizationRequiredTwilioCredentials) Resolve(sdkgo.Call) (messaging.Credentials, error) {
	return messaging.Credentials{}, sdkgo.NewReauthorizationRequiredError(errors.New("authorization revoked"))
}

type idempotencyHeaderTransport struct{ inner *http.Transport }

func (transport *idempotencyHeaderTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Method == http.MethodPost {
		if request.GetBody != nil {
			return nil, errors.New("fenced POST still has GetBody")
		}
		request.Header.Set("Idempotency-Key", "synthetic-transport-test")
	}
	return transport.inner.RoundTrip(request)
}

func (provider *recoveringTwilioCredentials) Resolve(sdkgo.Call) (messaging.Credentials, error) {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	provider.resolutions++
	if provider.resolutions == 1 {
		return messaging.Credentials{}, context.DeadlineExceeded
	}
	return provider.credentials, nil
}
