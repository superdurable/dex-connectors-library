// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package messaging_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/twilio/messaging"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const (
	// Split literals keep synthetic SIDs from matching repository secret scanning.
	testAccountSID       = "AC" + "0123456789abcdef0123456789abcdef"
	testAPIKeySID        = "SK" + "0123456789abcdef0123456789abcdef"
	testMessageSID       = "SMfedcba9876543210fedcba9876543210"
	testServiceSID       = "MG00112233445566778899aabbccddeeff"
	testSender           = "+14155550100"
	testRecipient        = "+14155550123"
	testAuthToken        = "auth-token-sentinel-0123456789ab"
	testAPIKeySecret     = "api-key-secret-sentinel-012345678"
	providerBodySentinel = "SENTINEL-BODY-TEXT"
	errorMessageSentinel = "SENTINEL-ERROR-MESSAGE"
)

var twilioConnection = sdkgo.ConnectionRef{Provider: "twilio", Name: "twilio-test"}

func TestSendMessagePostsOneFormRequestAndReturnsTheAcceptedMessage(t *testing.T) {
	provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Twilio-Request-Id", "RQ0123456789abcdef0123456789abcdef")
		response.WriteHeader(http.StatusCreated)
		writeBody(response, messageJSON(testMessageSID, "queued", "null"))
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())

	result, err := sdkgo.RunMutation(newTestDexContext("send-success"), client.SendMessage(), twilioConnection, messaging.SendMessageInput{
		To: testRecipient, Body: "Your table is ready.",
	})
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
	require.Nil(t, result.Failure)
	require.Equal(t, testMessageSID, result.Value.SID)
	require.Equal(t, messaging.MessageStatusQueued, result.Value.Status)
	require.Equal(t, testAccountSID, result.Value.AccountSID)
	require.Equal(t, testRecipient, result.Value.To)
	require.Equal(t, testSender, result.Value.From)
	require.Equal(t, 1, result.Value.SegmentCount)
	require.Equal(t, time.Date(2026, time.September, 30, 17, 4, 5, 0, time.UTC), result.Value.CreatedAt)
	require.True(t, result.Value.SentAt.IsZero())
	require.Equal(t, testMessageSID, result.Receipt.ProviderObjectID)
	require.Equal(t, "RQ0123456789abcdef0123456789abcdef", result.Receipt.ProviderRequestID)
	require.Equal(t, sdkgo.IdempotencyKey(result.Receipt.CallID), result.Receipt.IdempotencyKey)
	requireNoProviderText(t, result)

	requests := provider.recordedRequests()
	require.Len(t, requests, 1)
	require.Equal(t, http.MethodPost, requests[0].method)
	require.Equal(t, "/2010-04-01/Accounts/"+testAccountSID+"/Messages.json", requests[0].path)
	require.Equal(t, testAccountSID, requests[0].username)
	require.Equal(t, testAuthToken, requests[0].password)
	require.Equal(t, url.Values{"To": {testRecipient}, "From": {testSender}, "Body": {"Your table is ready."}}, requests[0].form)
	require.Empty(t, requests[0].header.Get("Idempotency-Key"), "Go treats a request with Idempotency-Key as replayable")
}

func TestSendMessageUsesAPIKeyMessagingServiceAndMedia(t *testing.T) {
	provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusCreated)
		writeBody(response, `{"sid":"MMfedcba9876543210fedcba9876543210","account_sid":"`+testAccountSID+`","messaging_service_sid":"`+testServiceSID+`","from":null,"to":"`+testRecipient+`","status":"accepted","direction":"outbound-api","error_code":null,"num_segments":"0","num_media":"1","price":null,"price_unit":null,"date_created":null,"date_sent":null,"date_updated":null}`)
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, apiKeyCredentials())

	result, err := sdkgo.RunMutation(newTestDexContext("send-service"), client.SendMessage(), twilioConnection, messaging.SendMessageInput{
		To: testRecipient, Sender: testServiceSID, MediaURLs: []string{"https://cdn.example.com/menu.png"},
	})
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
	require.Equal(t, messaging.MessageStatusAccepted, result.Value.Status)
	require.Equal(t, testServiceSID, result.Value.MessagingServiceSID)
	require.Empty(t, result.Value.From)
	require.Equal(t, 1, result.Value.MediaCount)

	requests := provider.recordedRequests()
	require.Len(t, requests, 1)
	require.Equal(t, testAPIKeySID, requests[0].username)
	require.Equal(t, testAPIKeySecret, requests[0].password)
	require.Equal(t, url.Values{
		"To": {testRecipient}, "MessagingServiceSid": {testServiceSID}, "MediaUrl": {"https://cdn.example.com/menu.png"},
	}, requests[0].form)
}

func TestSendMessageSendsWhatsAppFromAWhatsAppSender(t *testing.T) {
	provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusCreated)
		writeBody(response, strings.NewReplacer(`"to":"`+testRecipient, `"to":"whatsapp:`+testRecipient, `"from":"`+testSender, `"from":"whatsapp:`+testSender).
			Replace(messageJSON(testMessageSID, "queued", "null")))
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: "whatsapp:" + testSender}, authTokenCredentials())

	result, err := sdkgo.RunMutation(newTestDexContext("send-whatsapp"), client.SendMessage(), twilioConnection, messaging.SendMessageInput{
		To: "whatsapp:" + testRecipient, Body: "Your order shipped.",
	})
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchAccepted, result.Branch)
	require.Equal(t, "whatsapp:"+testRecipient, result.Value.To)
	require.Equal(t, "whatsapp:"+testSender, provider.recordedRequests()[0].form.Get("From"))
}

func TestSendMessageRejectsInvalidInputWithoutCallingTwilio(t *testing.T) {
	provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusCreated)
	})
	testCases := []struct {
		name          string
		defaultSender string
		input         messaging.SendMessageInput
	}{
		{"recipient without plus", testSender, messaging.SendMessageInput{To: "14155550123", Body: "hello"}},
		{"recipient with text", testSender, messaging.SendMessageInput{To: "+1415CALLNOW", Body: "hello"}},
		{"missing sender", "", messaging.SendMessageInput{To: testRecipient, Body: "hello"}},
		{"invalid sender", testSender, messaging.SendMessageInput{To: testRecipient, Body: "hello", Sender: "Acme"}},
		{"WhatsApp recipient from a phone number", testSender, messaging.SendMessageInput{To: "whatsapp:" + testRecipient, Body: "hello"}},
		{"phone recipient from a WhatsApp sender", testSender, messaging.SendMessageInput{To: testRecipient, Body: "hello", Sender: "whatsapp:" + testSender}},
		{"empty body without media", testSender, messaging.SendMessageInput{To: testRecipient, Body: "  "}},
		{"body over 1600 characters", testSender, messaging.SendMessageInput{To: testRecipient, Body: strings.Repeat("é", 1601)}},
		{"invalid UTF-8 body", testSender, messaging.SendMessageInput{To: testRecipient, Body: "\xff"}},
		{"plain HTTP media", testSender, messaging.SendMessageInput{To: testRecipient, MediaURLs: []string{"http://cdn.example.com/a.png"}}},
		{"media with credentials", testSender, messaging.SendMessageInput{To: testRecipient, MediaURLs: []string{"https://user:pass@cdn.example.com/a.png"}}},
		{"eleven media URLs", testSender, messaging.SendMessageInput{To: testRecipient, MediaURLs: repeatedMediaURLs(11)}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testCase.defaultSender}, authTokenCredentials())
			result, err := sdkgo.RunMutation(newTestDexContext("send-invalid"), client.SendMessage(), twilioConnection, testCase.input)
			require.NoError(t, err)
			require.Equal(t, messaging.SendMessageBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
			require.NotContains(t, result.Failure.Message, testRecipient)
		})
	}
	require.Empty(t, provider.recordedRequests())
}

func TestSendMessageClassifiesConclusiveRejections(t *testing.T) {
	testCases := []struct {
		name      string
		status    int
		errorCode int
		kind      sdkgo.FailureKind
	}{
		{"invalid To number", http.StatusBadRequest, 21211, sdkgo.FailureProviderRejection},
		{"unverified trial recipient", http.StatusBadRequest, 21608, sdkgo.FailureProviderRejection},
		{"unsubscribed recipient", http.StatusBadRequest, 21610, sdkgo.FailureProviderRejection},
		{"invalid credentials", http.StatusUnauthorized, 20003, sdkgo.FailureAuthentication},
		{"account without permission", http.StatusForbidden, 20008, sdkgo.FailureAuthorization},
		{"unknown account", http.StatusNotFound, 20404, sdkgo.FailureNotFound},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
				response.WriteHeader(testCase.status)
				writeBody(response, errorJSON(testCase.status, testCase.errorCode))
			})
			client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
			result, err := sdkgo.RunMutation(newTestDexContext("send-rejected"), client.SendMessage(), twilioConnection, validSendInput())
			require.NoError(t, err)
			require.Equal(t, messaging.SendMessageBranchProviderRejected, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			require.Equal(t, fmt.Sprintf("Twilio rejected the message with error %d", testCase.errorCode), result.Failure.Message)
			require.Equal(t, testCase.errorCode, result.Value.ErrorCode)
			require.Empty(t, result.Value.SID)
			require.Equal(t, testRecipient, result.Value.To)
			require.Equal(t, testSender, result.Value.From)
			require.Equal(t, fmt.Sprint(testCase.errorCode), result.Receipt.Metadata["twilioErrorCode"])
			require.Len(t, provider.recordedRequests(), 1)
			requireNoProviderText(t, result)
		})
	}
}

func TestSendMessageRetriesRateLimitWithRetryAfter(t *testing.T) {
	provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Retry-After", "7")
		response.WriteHeader(http.StatusTooManyRequests)
		writeBody(response, errorJSON(http.StatusTooManyRequests, 20429))
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	_, err := sdkgo.RunMutation(newTestDexContext("send-rate-limited"), client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Equal(t, sdkgo.FailureRateLimit, retry.Failure.Kind)
	var retryAfter *dex.RetryAfterError
	require.ErrorAs(t, err, &retryAfter)
	require.Equal(t, 7*time.Second, retryAfter.After)
	require.Len(t, provider.recordedRequests(), 1)
}

func TestSendMessageNeverRetriesAnOutcomeTwilioMayHaveAccepted(t *testing.T) {
	testCases := []struct {
		name    string
		respond func(http.ResponseWriter, *http.Request)
		config  messaging.Config
		kind    sdkgo.FailureKind
	}{
		{"internal server error", respondWithStatus(http.StatusInternalServerError), messaging.Config{}, sdkgo.FailureAvailability},
		{"service unavailable", respondWithStatus(http.StatusServiceUnavailable), messaging.Config{}, sdkgo.FailureAvailability},
		{"gateway request timeout", respondWithStatus(http.StatusRequestTimeout), messaging.Config{}, sdkgo.FailureAvailability},
		{"accepted without a usable message", func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusCreated)
			writeBody(response, `{"sid":"not-a-sid"}`)
		}, messaging.Config{}, sdkgo.FailureProtocol},
		{"accepted with an oversized response", func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusCreated)
			writeBody(response, messageJSON(testMessageSID, "queued", "null"))
		}, messaging.Config{MaxResponseBytes: 64}, sdkgo.FailureResponseTooLarge},
		{"accepted for another account", func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusCreated)
			writeBody(response, strings.ReplaceAll(messageJSON(testMessageSID, "queued", "null"), testAccountSID, "AC"+strings.Repeat("f", 32)))
		}, messaging.Config{}, sdkgo.FailureProtocol},
		{"accepted response that reflects the credential", func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusCreated)
			writeBody(response, strings.ReplaceAll(messageJSON(testMessageSID, "queued", "null"), `"to":"`+testRecipient+`"`, `"to":"`+testAuthToken+`"`))
		}, messaging.Config{}, sdkgo.FailureProtocol},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newTwilioFake(t, testCase.respond)
			testCase.config.DefaultSender = testSender
			client := newTwilioClient(t, provider.URL, testCase.config, authTokenCredentials())
			result, err := sdkgo.RunMutation(newTestDexContext("send-uncertain"), client.SendMessage(), twilioConnection, validSendInput())
			require.NoError(t, err, "an uncertain outcome is a branch, never a Retry")
			require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			require.Empty(t, result.Value.SID)
			require.Equal(t, testRecipient, result.Value.To)
			require.Len(t, provider.recordedRequests(), 1)
			requireNoProviderText(t, result)
		})
	}
}

func TestSendMessageTimeoutAfterDispatchSelectsUncertain(t *testing.T) {
	releaseProvider := make(chan struct{})
	provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
		<-releaseProvider
		response.WriteHeader(http.StatusCreated)
		writeBody(response, messageJSON(testMessageSID, "queued", "null"))
	})
	t.Cleanup(func() { close(releaseProvider) })
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials(),
		messaging.WithHTTPClient(&http.Client{Timeout: 200 * time.Millisecond}))

	result, err := sdkgo.RunMutation(newTestDexContext("send-timeout"), client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err, "a timeout after dispatch must not become a Dex Retry")
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
	require.Equal(t, sdkgo.FailureTransport, result.Failure.Kind)
	require.Equal(t, "Twilio message outcome is unknown", result.Failure.Message)
	require.Equal(t, testRecipient, result.Value.To)
	require.Len(t, provider.recordedRequests(), 1)
}

func TestSendMessageRetriesOnlyWhenNoConnectionOpened(t *testing.T) {
	closed := httptest.NewServer(http.NotFoundHandler())
	closedURL := closed.URL + "/2010-04-01"
	closed.Close()
	client := newTwilioClient(t, closedURL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	_, err := sdkgo.RunMutation(newTestDexContext("send-refused"), client.SendMessage(), twilioConnection, validSendInput())
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry, "a refused connection proves Twilio received nothing")
	require.Equal(t, sdkgo.FailureTransport, retry.Failure.Kind)
	require.NotContains(t, err.Error(), testAuthToken)

	opaqueTransport := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("transport failed with " + testAuthToken)
	})}
	client = newTwilioClient(t, "https://api.twilio.test/2010-04-01", messaging.Config{DefaultSender: testSender}, authTokenCredentials(),
		messaging.WithHTTPClient(opaqueTransport))
	result, err := sdkgo.RunMutation(newTestDexContext("send-opaque-transport"), client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch, "a transport without trace events cannot prove non-dispatch")
	require.NotContains(t, result.Failure.Message, testAuthToken)
}

func TestSendMessageDoesNotFollowRedirectsWithCredentials(t *testing.T) {
	var redirectedRequests int
	var mutex sync.Mutex
	destination := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		mutex.Lock()
		defer mutex.Unlock()
		redirectedRequests++
	}))
	defer destination.Close()
	provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
		response.Header().Set("Location", destination.URL)
		response.WriteHeader(http.StatusTemporaryRedirect)
	})
	client := newTwilioClient(t, provider.URL, messaging.Config{DefaultSender: testSender}, authTokenCredentials())
	result, err := sdkgo.RunMutation(newTestDexContext("send-redirect"), client.SendMessage(), twilioConnection, validSendInput())
	require.NoError(t, err)
	require.Equal(t, messaging.SendMessageBranchUncertain, result.Branch)
	mutex.Lock()
	defer mutex.Unlock()
	require.Zero(t, redirectedRequests)
}

func TestOperationsRequireUsableCredentialsBeforeCallingTwilio(t *testing.T) {
	provider := newTwilioFake(t, respondWithStatus(http.StatusCreated))
	testCases := []struct {
		name        string
		config      messaging.Config
		credentials sdkgo.CredentialProvider[messaging.Credentials]
		kind        sdkgo.FailureKind
	}{
		{"missing connection", messaging.Config{}, sdkgo.StaticCredentialProvider[messaging.Credentials]{}, sdkgo.FailureAuthentication},
		{"missing method", messaging.Config{}, staticCredentials(messaging.Credentials{AuthToken: sdkgo.NewSecretString(testAuthToken)}), sdkgo.FailureAuthentication},
		{"API key without its SID", messaging.Config{}, staticCredentials(messaging.Credentials{
			AuthMethodID: messaging.APIKeyAuthMethodID, APIKeySecret: sdkgo.NewSecretString(testAPIKeySecret),
		}), sdkgo.FailureAuthentication},
		{"API key with an account SID as its SID", messaging.Config{}, staticCredentials(messaging.Credentials{
			AuthMethodID: messaging.APIKeyAuthMethodID, APIKeySID: testAccountSID, APIKeySecret: sdkgo.NewSecretString(testAPIKeySecret),
		}), sdkgo.FailureAuthentication},
		{"secret with whitespace", messaging.Config{}, staticCredentials(messaging.Credentials{
			AuthMethodID: messaging.AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString("token with spaces"),
		}), sdkgo.FailureAuthentication},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			testCase.config.DefaultSender = testSender
			client := newTwilioClient(t, provider.URL, testCase.config, testCase.credentials)
			sendResult, err := sdkgo.RunMutation(newTestDexContext("send-credentials"), client.SendMessage(), twilioConnection, validSendInput())
			require.NoError(t, err)
			require.Equal(t, messaging.SendMessageBranchDefect, sendResult.Branch)
			require.Equal(t, testCase.kind, sendResult.Failure.Kind)
			getResult, err := sdkgo.RunQuery(newTestDexContext("get-credentials"), client.GetMessage(), twilioConnection, messaging.GetMessageInput{MessageSID: testMessageSID})
			require.NoError(t, err)
			require.Equal(t, messaging.GetMessageBranchDefect, getResult.Branch)
			require.Equal(t, testCase.kind, getResult.Failure.Kind)
		})
	}
	require.Empty(t, provider.recordedRequests())
}

func TestGetMessageReadsDeliveryStatusAndErrorCode(t *testing.T) {
	testCases := []struct {
		status    string
		errorCode string
		expected  int
	}{
		{"delivered", "null", 0},
		{"undelivered", "30003", 30003},
		{"failed", `"30008"`, 30008},
		{"a_future_status", "null", 0},
	}
	for _, testCase := range testCases {
		t.Run(testCase.status, func(t *testing.T) {
			provider := newTwilioFake(t, func(response http.ResponseWriter, _ *http.Request) {
				writeBody(response, messageJSON(testMessageSID, testCase.status, testCase.errorCode))
			})
			client := newTwilioClient(t, provider.URL, messaging.Config{}, authTokenCredentials())
			result, err := sdkgo.RunQuery(newTestDexContext("get-found"), client.GetMessage(), twilioConnection, messaging.GetMessageInput{MessageSID: " " + testMessageSID + " "})
			require.NoError(t, err)
			require.Equal(t, messaging.GetMessageBranchFound, result.Branch)
			require.Equal(t, messaging.MessageStatus(testCase.status), result.Value.Status)
			require.Equal(t, testCase.expected, result.Value.ErrorCode)
			require.Equal(t, "-0.00790", result.Value.Price)
			require.Equal(t, "USD", result.Value.PriceUnit)
			require.Equal(t, "outbound-api", result.Value.Direction)
			requireNoProviderText(t, result)
			requests := provider.recordedRequests()
			require.Len(t, requests, 1)
			require.Equal(t, http.MethodGet, requests[0].method)
			require.Equal(t, "/2010-04-01/Accounts/"+testAccountSID+"/Messages/"+testMessageSID+".json", requests[0].path)
			require.Equal(t, testAccountSID, requests[0].username)
		})
	}
}

func TestGetMessageClassifiesProviderOutcomes(t *testing.T) {
	testCases := []struct {
		name    string
		respond func(http.ResponseWriter, *http.Request)
		config  messaging.Config
		branch  sdkgo.BranchID
		kind    sdkgo.FailureKind
	}{
		{"missing message", func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusNotFound)
			writeBody(response, errorJSON(http.StatusNotFound, 20404))
		}, messaging.Config{}, messaging.GetMessageBranchNotFound, sdkgo.FailureNotFound},
		{"invalid credentials", func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusUnauthorized)
			writeBody(response, errorJSON(http.StatusUnauthorized, 20003))
		}, messaging.Config{}, messaging.GetMessageBranchProviderRejected, sdkgo.FailureAuthentication},
		{"test credentials cannot read", func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(http.StatusForbidden)
			writeBody(response, errorJSON(http.StatusForbidden, 20008))
		}, messaging.Config{}, messaging.GetMessageBranchProviderRejected, sdkgo.FailureAuthorization},
		{"oversized response", func(response http.ResponseWriter, _ *http.Request) {
			writeBody(response, messageJSON(testMessageSID, "delivered", "null"))
		}, messaging.Config{MaxResponseBytes: 64}, messaging.GetMessageBranchInvalidResponse, sdkgo.FailureResponseTooLarge},
		{"malformed response", func(response http.ResponseWriter, _ *http.Request) {
			writeBody(response, `{"sid":`)
		}, messaging.Config{}, messaging.GetMessageBranchInvalidResponse, sdkgo.FailureProtocol},
		{"another message", func(response http.ResponseWriter, _ *http.Request) {
			writeBody(response, messageJSON("SM00000000000000000000000000000000", "delivered", "null"))
		}, messaging.Config{}, messaging.GetMessageBranchInvalidResponse, sdkgo.FailureProtocol},
		{"trailing data", func(response http.ResponseWriter, _ *http.Request) {
			writeBody(response, messageJSON(testMessageSID, "delivered", "null")+"{}")
		}, messaging.Config{}, messaging.GetMessageBranchInvalidResponse, sdkgo.FailureProtocol},
		{"unexpected redirect", func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Location", "https://example.com")
			response.WriteHeader(http.StatusFound)
		}, messaging.Config{}, messaging.GetMessageBranchInvalidResponse, sdkgo.FailureProtocol},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newTwilioFake(t, testCase.respond)
			client := newTwilioClient(t, provider.URL, testCase.config, authTokenCredentials())
			result, err := sdkgo.RunQuery(newTestDexContext("get-outcome"), client.GetMessage(), twilioConnection, messaging.GetMessageInput{MessageSID: testMessageSID})
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			require.Equal(t, testCase.kind, result.Failure.Kind)
			requireNoProviderText(t, result)
		})
	}
}

func TestGetMessageRetriesTransientFailures(t *testing.T) {
	testCases := []struct {
		name    string
		respond func(http.ResponseWriter, *http.Request)
		kind    sdkgo.FailureKind
		delay   time.Duration
	}{
		{"rate limit with Retry-After", func(response http.ResponseWriter, _ *http.Request) {
			response.Header().Set("Retry-After", "3")
			response.WriteHeader(http.StatusTooManyRequests)
			writeBody(response, errorJSON(http.StatusTooManyRequests, 20429))
		}, sdkgo.FailureRateLimit, 3 * time.Second},
		{"rate limit without Retry-After", respondWithStatus(http.StatusTooManyRequests), sdkgo.FailureRateLimit, 0},
		{"server error", respondWithStatus(http.StatusBadGateway), sdkgo.FailureAvailability, 0},
		{"request timeout", respondWithStatus(http.StatusRequestTimeout), sdkgo.FailureAvailability, 0},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			provider := newTwilioFake(t, testCase.respond)
			client := newTwilioClient(t, provider.URL, messaging.Config{}, authTokenCredentials())
			_, err := sdkgo.RunQuery(newTestDexContext("get-retry"), client.GetMessage(), twilioConnection, messaging.GetMessageInput{MessageSID: testMessageSID})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, testCase.kind, retry.Failure.Kind)
			var retryAfter *dex.RetryAfterError
			if testCase.delay > 0 {
				require.ErrorAs(t, err, &retryAfter)
				require.Equal(t, testCase.delay, retryAfter.After)
			} else {
				require.False(t, errors.As(err, &retryAfter))
			}
		})
	}
}

func TestGetMessageRejectsInvalidSIDWithoutCallingTwilio(t *testing.T) {
	provider := newTwilioFake(t, respondWithStatus(http.StatusOK))
	client := newTwilioClient(t, provider.URL, messaging.Config{}, authTokenCredentials())
	for _, messageSID := range []string{"", "SM123", testServiceSID, "SM" + strings.Repeat("A", 32), testMessageSID + "/../x"} {
		result, err := sdkgo.RunQuery(newTestDexContext("get-invalid"), client.GetMessage(), twilioConnection, messaging.GetMessageInput{MessageSID: messageSID})
		require.NoError(t, err)
		require.Equal(t, messaging.GetMessageBranchDefect, result.Branch)
		require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
	}
	require.Empty(t, provider.recordedRequests())
}

func TestNewValidatesConnectionConfiguration(t *testing.T) {
	credentials := authTokenCredentials()
	testCases := []struct {
		name   string
		config messaging.Config
	}{
		{"missing account SID", messaging.Config{}},
		{"auth token pasted as account SID", messaging.Config{AccountSID: "0123456789abcdef0123456789abcdef"}},
		{"uppercase account SID", messaging.Config{AccountSID: strings.ToUpper(testAccountSID)}},
		{"invalid default sender", messaging.Config{AccountSID: testAccountSID, DefaultSender: "4155550100"}},
		{"plain HTTP endpoint", messaging.Config{AccountSID: testAccountSID, Endpoint: "http://api.twilio.com/2010-04-01"}},
		{"endpoint with query", messaging.Config{AccountSID: testAccountSID, Endpoint: "https://api.twilio.com/2010-04-01?x=1"}},
		{"negative response limit", messaging.Config{AccountSID: testAccountSID, MaxResponseBytes: -1}},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := messaging.New(testCase.config, credentials)
			require.Error(t, err)
		})
	}
	_, err := messaging.New(messaging.Config{AccountSID: testAccountSID}, nil)
	require.Error(t, err)
	client, err := messaging.New(messaging.Config{AccountSID: testAccountSID, DefaultSender: " " + testServiceSID + " "}, credentials)
	require.NoError(t, err)
	require.NotNil(t, client)
}

func TestDefaultConfigUsesTheUnitedStatesRegion(t *testing.T) {
	require.Equal(t, messaging.Config{Endpoint: "https://api.twilio.com/2010-04-01", MaxResponseBytes: 1 << 20}, messaging.DefaultConfig())
}

type recordedRequest struct {
	method   string
	path     string
	username string
	password string
	form     url.Values
	header   http.Header
}

type twilioFake struct {
	*httptest.Server
	mutex    sync.Mutex
	requests []recordedRequest
	respond  func(http.ResponseWriter, *http.Request)
}

func newTwilioFake(t *testing.T, respond func(http.ResponseWriter, *http.Request)) *twilioFake {
	t.Helper()
	provider := &twilioFake{respond: respond}
	provider.Server = httptest.NewServer(http.HandlerFunc(provider.serveHTTP))
	t.Cleanup(provider.Close)
	return provider
}

func (provider *twilioFake) serveHTTP(response http.ResponseWriter, request *http.Request) {
	username, password, _ := request.BasicAuth()
	if err := request.ParseForm(); err != nil {
		http.Error(response, "invalid form", http.StatusBadRequest)
		return
	}
	provider.mutex.Lock()
	provider.requests = append(provider.requests, recordedRequest{
		method: request.Method, path: request.URL.Path, username: username, password: password,
		form: request.PostForm, header: request.Header.Clone(),
	})
	provider.mutex.Unlock()
	provider.respond(response, request)
}

func (provider *twilioFake) recordedRequests() []recordedRequest {
	provider.mutex.Lock()
	defer provider.mutex.Unlock()
	return append([]recordedRequest(nil), provider.requests...)
}

func newTwilioClient(
	t *testing.T,
	endpoint string,
	config messaging.Config,
	credentials sdkgo.CredentialProvider[messaging.Credentials],
	options ...messaging.Option,
) *messaging.Client {
	t.Helper()
	if !strings.HasSuffix(endpoint, "/2010-04-01") {
		endpoint += "/2010-04-01"
	}
	config.Endpoint = endpoint
	config.AccountSID = testAccountSID
	client, err := messaging.New(config, credentials, options...)
	require.NoError(t, err)
	return client
}

func authTokenCredentials() sdkgo.StaticCredentialProvider[messaging.Credentials] {
	return staticCredentials(messaging.Credentials{
		AuthMethodID: messaging.AuthTokenAuthMethodID, AuthToken: sdkgo.NewSecretString(testAuthToken),
	})
}

func apiKeyCredentials() sdkgo.StaticCredentialProvider[messaging.Credentials] {
	return staticCredentials(messaging.Credentials{
		AuthMethodID: messaging.APIKeyAuthMethodID, APIKeySID: testAPIKeySID, APIKeySecret: sdkgo.NewSecretString(testAPIKeySecret),
	})
}

func staticCredentials(credentials messaging.Credentials) sdkgo.StaticCredentialProvider[messaging.Credentials] {
	return sdkgo.StaticCredentialProvider[messaging.Credentials]{twilioConnection: credentials}
}

func validSendInput() messaging.SendMessageInput {
	return messaging.SendMessageInput{To: testRecipient, Body: "Your table is ready."}
}

func repeatedMediaURLs(count int) []string {
	mediaURLs := make([]string, count)
	for index := range mediaURLs {
		mediaURLs[index] = fmt.Sprintf("https://cdn.example.com/%d.png", index)
	}
	return mediaURLs
}

func messageJSON(messageSID string, status string, errorCode string) string {
	return `{"account_sid":"` + testAccountSID + `","api_version":"2010-04-01","body":"` + providerBodySentinel + `",` +
		`"date_created":"Wed, 30 Sep 2026 17:04:05 +0000","date_sent":null,"date_updated":"Wed, 30 Sep 2026 17:04:06 +0000",` +
		`"direction":"outbound-api","error_code":` + errorCode + `,"error_message":"` + errorMessageSentinel + `",` +
		`"from":"` + testSender + `","messaging_service_sid":null,"num_media":"0","num_segments":"1",` +
		`"price":"-0.00790","price_unit":"USD","sid":"` + messageSID + `","status":"` + status + `",` +
		`"subresource_uris":{"media":"/2010-04-01/Accounts/` + testAccountSID + `/Messages/` + messageSID + `/Media.json"},` +
		`"to":"` + testRecipient + `","uri":"/2010-04-01/Accounts/` + testAccountSID + `/Messages/` + messageSID + `.json"}`
}

func errorJSON(status int, errorCode int) string {
	return fmt.Sprintf(`{"code":%d,"message":"%s","more_info":"https://www.twilio.com/docs/errors/%d","status":%d}`,
		errorCode, errorMessageSentinel, errorCode, status)
}

func respondWithStatus(status int) func(http.ResponseWriter, *http.Request) {
	return func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(status)
		writeBody(response, errorJSON(status, 0))
	}
}

func writeBody(response http.ResponseWriter, body string) {
	if _, err := response.Write([]byte(body)); err != nil {
		panic(err)
	}
}

// requireNoProviderText proves message text, provider error text, and secrets never enter a Result.
func requireNoProviderText(t *testing.T, result any) {
	t.Helper()
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	for _, forbidden := range []string{providerBodySentinel, errorMessageSentinel, testAuthToken, testAPIKeySecret} {
		require.NotContains(t, string(encoded), forbidden)
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (function roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return function(request)
}

type testDexContext struct {
	context.Context
	stepExecutionID string
}

func newTestDexContext(stepExecutionID string) *testDexContext {
	return &testDexContext{Context: context.Background(), stepExecutionID: stepExecutionID}
}

func (*testDexContext) FlowID() string                                  { return "twilio-test-flow" }
func (*testDexContext) RunID() string                                   { return "run" }
func (*testDexContext) FlowStartedAt() time.Time                        { return time.Unix(1, 0) }
func (context *testDexContext) StepExecutionID() string                 { return context.stepExecutionID }
func (*testDexContext) FromStepExecutionID() string                     { return "" }
func (*testDexContext) RecoveryError() *dex.RecoveryErrorInfo           { return nil }
func (*testDexContext) FirstAttemptAt() time.Time                       { return time.Unix(1, 0) }
func (*testDexContext) Attempt() int32                                  { return 1 }
func (*testDexContext) HasTimerFired() bool                             { return false }
func (*testDexContext) HasTimerFiredByIndex(int) bool                   { return false }
func (*testDexContext) WaitForMethodFailed() bool                       { return false }
func (*testDexContext) RecordHeartbeat(any) error                       { return nil }
func (*testDexContext) GetLastHeartbeatValue(any) (bool, error)         { return false, nil }
func (*testDexContext) SetStepExecutionLocal(string, any) error         { return nil }
func (*testDexContext) GetStepExecutionLocal(string, any) (bool, error) { return false, nil }
func (*testDexContext) RecordEvent(string, any) error                   { return nil }

var _ dex.Context = (*testDexContext)(nil)
