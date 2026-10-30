// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendly_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/calendly"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const testCallbackURL = "https://hooks.example.com/webhooks/calendly"

// fakeSubscriptions is Calendly's webhook subscription store: a create with a listed URL answers 409.
type fakeSubscriptions struct {
	mu            sync.Mutex
	subscriptions []map[string]any
	isListLagging bool
}

func (store *fakeSubscriptions) routes() map[string]http.HandlerFunc {
	return map[string]http.HandlerFunc{
		"GET /users/me": currentUserRoute(),
		"GET /webhook_subscriptions": func(response http.ResponseWriter, _ *http.Request) {
			store.mu.Lock()
			defer store.mu.Unlock()
			collection := store.subscriptions
			if store.isListLagging {
				collection, store.isListLagging = nil, false
			}
			encoded, err := json.Marshal(map[string]any{"collection": collection, "pagination": map[string]any{"next_page_token": nil}})
			if err != nil {
				panic(err)
			}
			writeJSON(response, http.StatusOK, string(encoded))
		},
		"POST /webhook_subscriptions": func(response http.ResponseWriter, request *http.Request) {
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				writeJSON(response, http.StatusBadRequest, providerError("Invalid Argument", "json"))
				return
			}
			store.mu.Lock()
			defer store.mu.Unlock()
			for _, existing := range store.subscriptions {
				if existing["callback_url"] == body["url"] {
					writeJSON(response, http.StatusConflict, providerError("Already Exists", "Hook with this url already exists"))
					return
				}
			}
			subscription := map[string]any{
				"uri": fmt.Sprintf("https://api.calendly.com/webhook_subscriptions/HOOK%04d", len(store.subscriptions)+1), "callback_url": body["url"],
				"state": "active", "events": body["events"], "scope": body["scope"], "organization": body["organization"], "user": body["user"],
				"created_at": "2026-09-30T12:00:00.000000Z", "updated_at": "2026-09-30T12:00:00.000000Z", "retry_started_at": nil, "creator": testUserURI,
			}
			store.subscriptions = append(store.subscriptions, subscription)
			encoded, err := json.Marshal(map[string]any{"resource": subscription})
			if err != nil {
				panic(err)
			}
			writeJSON(response, http.StatusCreated, string(encoded))
		},
	}
}

func createWebhookSubscription(
	t *testing.T, client *calendly.Client, input calendly.CreateWebhookSubscriptionInput,
) calendly.CreateWebhookSubscriptionResult {
	t.Helper()
	result, err := sdkgo.RunMutation(newStepContext("subscribe"), client.CreateWebhookSubscription(), testConnection, input)
	require.NoError(t, err)
	requireSecretFree(t, result)
	return result
}

func TestCreateWebhookSubscriptionCreatesOnceAndReusesItOnRepeat(t *testing.T) {
	store := &fakeSubscriptions{}
	fake := newFakeCalendly(t, store.routes())
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(sentinelSigningKey), calendly.Config{})

	created := createWebhookSubscription(t, client, calendly.CreateWebhookSubscriptionInput{CallbackURL: testCallbackURL})
	require.Equal(t, calendly.CreateWebhookSubscriptionBranchSubscribed, created.Branch)
	require.True(t, created.Value.WasCreated)
	require.Equal(t, calendly.WebhookSubscription{
		URI: "https://api.calendly.com/webhook_subscriptions/HOOK0001", CallbackURL: testCallbackURL, State: "active",
		Events: []string{"invitee.created", "invitee.canceled"}, Scope: "user", OrganizationURI: testOrganizationURI, UserURI: testUserURI,
		CreatedAt: fixedNow, WasCreated: true,
	}, created.Value)

	repeated := createWebhookSubscription(t, client, calendly.CreateWebhookSubscriptionInput{CallbackURL: testCallbackURL})
	require.Equal(t, calendly.CreateWebhookSubscriptionBranchSubscribed, repeated.Branch)
	require.False(t, repeated.Value.WasCreated, "the repeat finds the subscription before creating")
	require.Equal(t, created.Value.URI, repeated.Value.URI)

	posts := fake.requestsTo(http.MethodPost, "/webhook_subscriptions")
	require.Len(t, posts, 1)
	var body map[string]any
	require.NoError(t, json.Unmarshal([]byte(posts[0].body), &body))
	require.Equal(t, map[string]any{
		"url": testCallbackURL, "events": []any{"invitee.created", "invitee.canceled"}, "organization": testOrganizationURI,
		"scope": "user", "user": testUserURI, "signing_key": sentinelSigningKey,
	}, body, "a personal access token connection registers its own signing key")
	lists := fake.requestsTo(http.MethodGet, "/webhook_subscriptions")
	require.Len(t, lists, 2)
	require.Equal(t, map[string][]string{
		"organization": {testOrganizationURI}, "scope": {"user"}, "user": {testUserURI}, "count": {"100"}, "sort": {"created_at:asc"},
	}, map[string][]string(lists[0].query))
}

func TestCreateWebhookSubscriptionReusesTheSubscriptionAConcurrentAttemptCreated(t *testing.T) {
	store := &fakeSubscriptions{isListLagging: true, subscriptions: []map[string]any{{
		"uri": "https://api.calendly.com/webhook_subscriptions/HOOK0007", "callback_url": testCallbackURL, "state": "active",
		"events": []string{"invitee.created", "invitee.canceled"}, "scope": "organization", "organization": testOrganizationURI, "user": nil,
		"created_at": "2026-09-30T11:00:00.000000Z",
	}}}
	fake := newFakeCalendly(t, store.routes())
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(sentinelSigningKey), calendly.Config{})
	result := createWebhookSubscription(t, client, calendly.CreateWebhookSubscriptionInput{
		CallbackURL: testCallbackURL, Scope: calendly.WebhookSubscriptionScopeOrganization, Events: []string{calendly.WebhookEventInviteeCreated},
		OrganizationURI: testOrganizationURI,
	})
	require.Equal(t, calendly.CreateWebhookSubscriptionBranchSubscribed, result.Branch, "the 409 lists again and finds it")
	require.False(t, result.Value.WasCreated)
	require.Equal(t, "https://api.calendly.com/webhook_subscriptions/HOOK0007", result.Value.URI)
	require.Len(t, fake.requestsTo(http.MethodPost, "/webhook_subscriptions"), 1)
	require.Empty(t, fake.requestsTo(http.MethodGet, "/users/me"), "the input named the organization")
}

func TestCreateWebhookSubscriptionReportsAConflictOutsideTheListedScopeWithoutRetrying(t *testing.T) {
	fake := newFakeCalendly(t, map[string]http.HandlerFunc{
		"GET /users/me":               currentUserRoute(),
		"GET /webhook_subscriptions":  respondJSON(http.StatusOK, `{"collection":[],"pagination":{"next_page_token":null}}`),
		"POST /webhook_subscriptions": respondJSON(http.StatusConflict, providerError("Already Exists", "Hook with this url already exists")),
	})
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(sentinelSigningKey), calendly.Config{})
	result := createWebhookSubscription(t, client, calendly.CreateWebhookSubscriptionInput{CallbackURL: testCallbackURL})
	require.Equal(t, calendly.CreateWebhookSubscriptionBranchConflictingSubscription, result.Branch, "another scope owns the URL")
	require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
	require.Len(t, fake.requestsTo(http.MethodGet, "/webhook_subscriptions"), 2)
}

func TestCreateWebhookSubscriptionReportsADisabledOrNarrowerSubscriptionAsConflicting(t *testing.T) {
	for _, test := range []struct {
		name   string
		state  string
		events []string
	}{
		{"disabled", "disabled", []string{"invitee.created", "invitee.canceled"}},
		{"missing an event", "active", []string{"invitee.created"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			store := &fakeSubscriptions{subscriptions: []map[string]any{{
				"uri": "https://api.calendly.com/webhook_subscriptions/HOOK0003", "callback_url": testCallbackURL, "state": test.state,
				"events": test.events, "scope": "user", "organization": testOrganizationURI, "user": testUserURI,
			}}}
			fake := newFakeCalendly(t, store.routes())
			client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(sentinelSigningKey), calendly.Config{})
			result := createWebhookSubscription(t, client, calendly.CreateWebhookSubscriptionInput{CallbackURL: testCallbackURL})
			require.Equal(t, calendly.CreateWebhookSubscriptionBranchConflictingSubscription, result.Branch)
			require.Equal(t, sdkgo.FailureConflict, result.Failure.Kind)
			require.Equal(t, "https://api.calendly.com/webhook_subscriptions/HOOK0003", result.Value.URI)
			require.Empty(t, fake.requestsTo(http.MethodPost, "/webhook_subscriptions"))
		})
	}
}

func TestCreateWebhookSubscriptionLeavesTheSigningKeyToAnOAuthApp(t *testing.T) {
	store := &fakeSubscriptions{}
	fake := newFakeCalendly(t, store.routes())
	credentials := sdkgo.StaticCredentialProvider[calendly.Credentials]{testConnection: {
		AuthMethodID: calendly.CalendlyOAuthAuthMethodID, AccessToken: sdkgo.NewSecretString(sentinelToken),
	}}
	result := createWebhookSubscription(t, newTestClient(t, fake.redirectingClient(), credentials, calendly.Config{}),
		calendly.CreateWebhookSubscriptionInput{CallbackURL: testCallbackURL})
	require.Equal(t, calendly.CreateWebhookSubscriptionBranchSubscribed, result.Branch)
	require.NotContains(t, fake.requestsTo(http.MethodPost, "/webhook_subscriptions")[0].body, "signing_key")
}

func TestCreateWebhookSubscriptionSelectsDefectOrRejectionWithoutCreating(t *testing.T) {
	fake := newFakeCalendly(t, (&fakeSubscriptions{}).routes())
	client := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(sentinelSigningKey), calendly.Config{})
	for _, test := range []struct {
		name           string
		input          calendly.CreateWebhookSubscriptionInput
		expectedReason string
	}{
		{"plain HTTP", calendly.CreateWebhookSubscriptionInput{CallbackURL: "http://hooks.example.com/calendly"}, "HTTPS"},
		{"credentials in URL", calendly.CreateWebhookSubscriptionInput{CallbackURL: "https://user:pass@hooks.example.com/calendly"}, "user information"},
		{"unsupported event", calendly.CreateWebhookSubscriptionInput{CallbackURL: testCallbackURL, Events: []string{"routing_form_submission.created"}}, "events"},
		{"duplicate event", calendly.CreateWebhookSubscriptionInput{CallbackURL: testCallbackURL, Events: []string{"invitee.created", "invitee.created"}}, "distinct"},
		{"group scope", calendly.CreateWebhookSubscriptionInput{CallbackURL: testCallbackURL, Scope: "group"}, "scope"},
		{"user on an organization scope", calendly.CreateWebhookSubscriptionInput{CallbackURL: testCallbackURL, Scope: "organization", UserURI: testUserURI}, "userUri"},
	} {
		t.Run(test.name, func(t *testing.T) {
			result := createWebhookSubscription(t, client, test.input)
			require.Equal(t, calendly.CreateWebhookSubscriptionBranchDefect, result.Branch)
			require.Contains(t, result.Failure.Message, test.expectedReason)
		})
	}
	unsigned := newTestClient(t, fake.redirectingClient(), personalAccessTokenCredentials(""), calendly.Config{})
	result := createWebhookSubscription(t, unsigned, calendly.CreateWebhookSubscriptionInput{CallbackURL: testCallbackURL})
	require.Equal(t, calendly.CreateWebhookSubscriptionBranchDefect, result.Branch)
	require.Contains(t, result.Failure.Message, "webhook_signing_key", "an unsigned subscription could never be verified")
	require.Empty(t, fake.recordedRequests())

	forbidden := newFakeCalendly(t, map[string]http.HandlerFunc{
		"GET /webhook_subscriptions": respondJSON(http.StatusForbidden, providerError("Permission Denied", "Please upgrade your Calendly account to Standard")),
	})
	rejected := createWebhookSubscription(t, newTestClient(t, forbidden.redirectingClient(), personalAccessTokenCredentials(sentinelSigningKey), calendly.Config{}),
		calendly.CreateWebhookSubscriptionInput{CallbackURL: testCallbackURL, OrganizationURI: testOrganizationURI, UserURI: testUserURI})
	require.Equal(t, calendly.CreateWebhookSubscriptionBranchProviderRejected, rejected.Branch)
	require.Equal(t, sdkgo.FailureAuthorization, rejected.Failure.Kind)
	require.Empty(t, forbidden.requestsTo(http.MethodPost, "/webhook_subscriptions"))
}
