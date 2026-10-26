// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestSendMessageFenceAllowsOneRetryAfterProviderRejection(t *testing.T) {
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
			var marker singledispatch.Marker
			hasMarker, err := ctx.GetLastHeartbeatValue(&marker)
			require.NoError(t, err)
			require.True(t, hasMarker)
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

func TestSendMessageFenceFailsClosedWhenClaimIsLost(t *testing.T) {
	var requests int
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		requests++
		return nil, errors.New("reply was lost")
	})
	client := newTwilioClient(t, "https://api.twilio.test/2010-04-01", messaging.Config{DefaultSender: testSender}, authTokenCredentials(), messaging.WithHTTPClient(&http.Client{Transport: transport}))
	ctx := singledispatchtest.NewContext("twilio-fence-lost-claim", "send-message")
	ctx.LoseNextHeartbeat()
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
	require.Equal(t, 1, requests)
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
	provider := newTwilioFake(t, respondWithStatus(http.StatusCreated))
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
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
	require.Empty(t, provider.recordedRequests())
}

func TestSendMessageFenceReadsBackAcceptedMessageWithoutResend(t *testing.T) {
	for _, readStatus := range []int{http.StatusOK, http.StatusServiceUnavailable} {
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
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
	require.Len(t, provider.recordedRequests(), 1)
}

func TestSendMessageFenceDoesNotResendAnAcceptedBodyWithoutSID(t *testing.T) {
	provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusCreated)
		writeBody(response, `{"status":"queued"}`)
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	ctx := singledispatchtest.NewContext("twilio-fence-no-sid", "send-message")
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
	ctx.AdvanceAttempt()
	result, err = sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
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
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
	require.EqualValues(t, 1, posts.Load())
}

func TestSendMessageFenceRejectsStaleResendMarker(t *testing.T) {
	provider := newTwilioFake(t, respondWithStatus(http.StatusServiceUnavailable))
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	ctx := singledispatchtest.NewContext("twilio-fence-stale", "send-message")
	_, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
	require.Len(t, provider.recordedRequests(), 1)
}

func TestSendMessageFenceRejectsForeignCheckpoint(t *testing.T) {
	provider := newTwilioFake(t, respondWithStatus(http.StatusCreated))
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	ctx := singledispatchtest.NewContext("twilio-fence-foreign", "send-message")
	marker := singledispatch.Marker{CallID: sdkgo.CallID("foreign-call"), Attempt: 1, State: singledispatch.StateCanResend}
	require.NoError(t, ctx.SetPersistedCheckpoint(marker))
	ctx.AdvanceAttempt()
	result, err := sdkgo.RunMutation(ctx, client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
	require.Empty(t, provider.recordedRequests())
}

type recoveringTwilioCredentials struct {
	mutex       sync.Mutex
	resolutions int
	credentials messaging.Credentials
}

type rotatingTwilioCredentials struct {
	mutex       sync.Mutex
	resolutions int
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
