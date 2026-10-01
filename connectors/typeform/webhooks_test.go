// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package typeform_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/typeform"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	testWebhookTag  = "dex-recorder"
	testWebhookURL  = "https://hooks.example.com/webhooks/typeform"
	testWebhookPath = "/forms/lT4Z3j/webhooks/dex-recorder"
)

// fakeWebhooks stores PUT /forms/{form_id}/webhooks/{tag} the way Typeform documents it: one webhook per tag.
type fakeWebhooks struct {
	mu       sync.Mutex
	webhooks map[string]map[string]any
}

func newFakeWebhooks() *fakeWebhooks {
	return &fakeWebhooks{webhooks: map[string]map[string]any{}}
}

// upsertRoute stores the request and echoes the webhook, secret included, as Typeform's schema shows.
func (store *fakeWebhooks) upsertRoute(formID string, tag string) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			writeJSON(response, http.StatusBadRequest, providerError("VALIDATION_ERROR"))
			return
		}
		store.mu.Lock()
		existing, isFound := store.webhooks[tag]
		createdAt := "2026-09-30T12:00:00.000Z"
		if isFound {
			createdAt = existing["created_at"].(string)
		}
		webhook := map[string]any{
			"id": "yRtagDm8AT", "form_id": formID, "tag": tag, "url": body["url"], "enabled": body["enabled"],
			"secret": body["secret"], "event_types": body["event_types"], "verify_ssl": true,
			"created_at": createdAt, "updated_at": "2026-09-30T12:00:01.000Z",
		}
		store.webhooks[tag] = webhook
		store.mu.Unlock()
		encoded, err := json.Marshal(webhook)
		if err != nil {
			panic(err)
		}
		writeJSON(response, http.StatusOK, string(encoded))
	}
}

func (store *fakeWebhooks) count() int {
	store.mu.Lock()
	defer store.mu.Unlock()
	return len(store.webhooks)
}

func TestUpsertWebhookPutsTheSecretAndReturnsTheWebhookWithoutIt(t *testing.T) {
	store := newFakeWebhooks()
	fake := newFakeTypeform(t, map[string]http.HandlerFunc{"PUT " + testWebhookPath: store.upsertRoute(testFormID, testWebhookTag)})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(sentinelSecret), typeform.Config{DataCenter: typeform.DataCenterNewEu})
	input := typeform.UpsertWebhookInput{FormID: testFormID, Tag: testWebhookTag, URL: testWebhookURL}

	result, err := sdkgo.RunMutation(newStepContext("upsert"), client.UpsertWebhook(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, typeform.UpsertWebhookBranchUpserted, result.Branch)
	require.Equal(t, typeform.Webhook{
		ID: "yRtagDm8AT", FormID: testFormID, Tag: testWebhookTag, URL: testWebhookURL, IsEnabled: true,
		EventTypes: []string{typeform.WebhookEventTypeFormResponse},
		CreatedAt:  time.Date(2026, time.September, 30, 12, 0, 0, 0, time.UTC), UpdatedAt: time.Date(2026, time.September, 30, 12, 0, 1, 0, time.UTC),
	}, result.Value)
	requireSecretFree(t, result)
	requests := fake.requestsTo(http.MethodPut, testWebhookPath)
	require.Len(t, requests, 1)
	require.Equal(t, "api.typeform.eu", requests[0].host)
	require.Equal(t, "Bearer "+sentinelToken, requests[0].authorization)
	require.JSONEq(t, fmt.Sprintf(`{"url":%q,"enabled":true,"secret":%q,"event_types":{"form_response":true}}`, testWebhookURL, sentinelSecret), requests[0].body)

	// The same tag updates the same webhook: disabling it leaves one webhook, now disabled.
	input.IsDisabled = true
	result, err = sdkgo.RunMutation(newStepContext("disable"), client.UpsertWebhook(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, typeform.UpsertWebhookBranchUpserted, result.Branch)
	require.False(t, result.Value.IsEnabled)
	require.Equal(t, 1, store.count())
	require.JSONEq(t, fmt.Sprintf(`{"url":%q,"enabled":false,"secret":%q,"event_types":{"form_response":true}}`, testWebhookURL, sentinelSecret),
		fake.requestsTo(http.MethodPut, testWebhookPath)[1].body)
}

func TestUpsertWebhookEscapesTheTagAsOnePathSegment(t *testing.T) {
	store := newFakeWebhooks()
	fake := newFakeTypeform(t, map[string]http.HandlerFunc{"PUT /forms/lT4Z3j/webhooks/v1.recorder_a-b": store.upsertRoute(testFormID, "v1.recorder_a-b")})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(sentinelSecret), typeform.Config{})
	result, err := sdkgo.RunMutation(newStepContext("upsert"), client.UpsertWebhook(), testConnection,
		typeform.UpsertWebhookInput{FormID: testFormID, Tag: "v1.recorder_a-b", URL: testWebhookURL})
	require.NoError(t, err)
	require.Equal(t, typeform.UpsertWebhookBranchUpserted, result.Branch)
}

func TestUpsertWebhookSelectsDefectBeforeAnyRequest(t *testing.T) {
	valid := typeform.UpsertWebhookInput{FormID: testFormID, Tag: testWebhookTag, URL: testWebhookURL}
	for _, test := range []struct {
		name   string
		input  typeform.UpsertWebhookInput
		secret string
	}{
		{"blank webhook secret", valid, ""},
		{"http URL", typeform.UpsertWebhookInput{FormID: testFormID, Tag: testWebhookTag, URL: "http://hooks.example.com/typeform"}, sentinelSecret},
		{"URL with user information", typeform.UpsertWebhookInput{FormID: testFormID, Tag: testWebhookTag, URL: "https://user:pass@hooks.example.com/"}, sentinelSecret},
		{"URL with a fragment", typeform.UpsertWebhookInput{FormID: testFormID, Tag: testWebhookTag, URL: testWebhookURL + "#x"}, sentinelSecret},
		{"tag with a slash", typeform.UpsertWebhookInput{FormID: testFormID, Tag: "a/b", URL: testWebhookURL}, sentinelSecret},
		{"blank tag", typeform.UpsertWebhookInput{FormID: testFormID, URL: testWebhookURL}, sentinelSecret},
		{"invalid form ID", typeform.UpsertWebhookInput{FormID: "lT4Z3j?x=1", Tag: testWebhookTag, URL: testWebhookURL}, sentinelSecret},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeTypeform(t, nil)
			client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(test.secret), typeform.Config{})
			result, err := sdkgo.RunMutation(newStepContext("upsert"), client.UpsertWebhook(), testConnection, test.input)
			require.NoError(t, err)
			require.Equal(t, typeform.UpsertWebhookBranchDefect, result.Branch)
			require.Equal(t, sdkgo.FailureValidation, result.Failure.Kind)
			require.Empty(t, fake.recordedRequests())
			requireSecretFree(t, result)
		})
	}
}

func TestUpsertWebhookClassifiesTypeformAnswers(t *testing.T) {
	for _, test := range []struct {
		name           string
		handler        http.HandlerFunc
		expectedBranch sdkgo.BranchID
		expectedKind   sdkgo.FailureKind
	}{
		{"http URL refused", respondJSON(http.StatusBadRequest, providerError("webhook_url_https_required")), typeform.UpsertWebhookBranchProviderRejected, sdkgo.FailureValidation},
		{"plan without webhooks", respondJSON(http.StatusPaymentRequired, providerError("PAYMENT_REQUIRED")), typeform.UpsertWebhookBranchProviderRejected, sdkgo.FailureProviderRejection},
		{"missing scope", respondJSON(http.StatusForbidden, providerError("AUTHENTICATION_ERROR")), typeform.UpsertWebhookBranchProviderRejected, sdkgo.FailureAuthorization},
		{"missing form", respondJSON(http.StatusNotFound, providerError("NOT_EXISTING_ID")), typeform.UpsertWebhookBranchNotFound, sdkgo.FailureNotFound},
		{"unreadable 2xx", respondJSON(http.StatusOK, `{"id":`), typeform.UpsertWebhookBranchInvalidResponse, sdkgo.FailureProtocol},
		{"another tag in the answer", newFakeWebhooks().upsertRoute(testFormID, "other-tag"), typeform.UpsertWebhookBranchInvalidResponse, sdkgo.FailureProtocol},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeTypeform(t, map[string]http.HandlerFunc{"PUT " + testWebhookPath: test.handler})
			client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(sentinelSecret), typeform.Config{})
			result, err := sdkgo.RunMutation(newStepContext("upsert"), client.UpsertWebhook(), testConnection,
				typeform.UpsertWebhookInput{FormID: testFormID, Tag: testWebhookTag, URL: testWebhookURL})
			require.NoError(t, err)
			require.Equal(t, test.expectedBranch, result.Branch)
			require.Equal(t, test.expectedKind, result.Failure.Kind)
			requireSecretFree(t, result)
		})
	}
}

func TestUpsertWebhookRetriesAServerErrorOrALostAnswerBecauseThePutIsIdempotent(t *testing.T) {
	for _, test := range []struct {
		name         string
		handler      http.HandlerFunc
		expectedKind sdkgo.FailureKind
	}{
		{"server error", respondJSON(http.StatusInternalServerError, providerError("SERVER_ERROR")), sdkgo.FailureAvailability},
		{"lost answer", func(response http.ResponseWriter, _ *http.Request) {
			connection, _, err := response.(http.Hijacker).Hijack()
			if err == nil {
				_ = connection.Close() // Closing without an answer is the behavior under test.
			}
		}, sdkgo.FailureTransport},
	} {
		t.Run(test.name, func(t *testing.T) {
			fake := newFakeTypeform(t, map[string]http.HandlerFunc{"PUT " + testWebhookPath: test.handler})
			client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(sentinelSecret), typeform.Config{})
			_, err := sdkgo.RunMutation(newStepContext("upsert"), client.UpsertWebhook(), testConnection,
				typeform.UpsertWebhookInput{FormID: testFormID, Tag: testWebhookTag, URL: testWebhookURL})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, test.expectedKind, retry.Failure.Kind)
			require.NotContains(t, err.Error(), sentinelSecret)
		})
	}
}
