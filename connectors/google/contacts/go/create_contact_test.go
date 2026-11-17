// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contacts

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestCreateContactConfirmsOneSendAndReadsBackTheSavedContact(t *testing.T) {
	requests, posts := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, "names,emailAddresses", r.URL.Query().Get("personFields"))
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		if r.Method == http.MethodGet {
			require.Equal(t, "/v1/people/c123", r.URL.Path)
			_, err := w.Write([]byte(`{"resourceName":"people/c123","names":[{"displayName":"Ada Lovelace"}],"emailAddresses":[{"value":"ada@example.com"}]}`))
			require.NoError(t, err)
			return
		}
		posts++
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/people:createContact", r.URL.Path)
		var body struct {
			Names []struct {
				GivenName  string `json:"givenName"`
				FamilyName string `json:"familyName"`
			} `json:"names"`
			EmailAddresses []struct {
				Value string `json:"value"`
			} `json:"emailAddresses"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&body))
		require.Len(t, body.Names, 1)
		require.Equal(t, "Ada", body.Names[0].GivenName)
		require.Equal(t, "Lovelace", body.Names[0].FamilyName)
		require.Equal(t, "ada@example.com", body.EmailAddresses[0].Value)
		_, err := w.Write([]byte(`{"resourceName":"people/c123","names":[{"displayName":"Ada Lovelace"}],"emailAddresses":[{"value":"ada@example.com"}],"phoneNumbers":[{"value":"discard"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("create-contact-flow", "create-contact-step")
	input := CreateContactInput{GivenName: "Ada", FamilyName: "Lovelace", EmailAddress: "ada@example.com"}
	created, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchCreated, created.Branch)
	require.Equal(t, Contact{ResourceName: "people/c123", DisplayName: "Ada Lovelace", EmailAddress: "ada@example.com"}, created.Value)
	ctx.AdvanceAttempt()
	readBack, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchCreated, readBack.Branch)
	require.Equal(t, created.Value, readBack.Value)
	require.Equal(t, 2, requests)
	require.Equal(t, 1, posts)
}

func TestCreateContactRetriesOnlyDefinitiveRejections(t *testing.T) {
	status, requests := http.StatusTooManyRequests, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(status)
		if status == http.StatusOK {
			_, err := w.Write([]byte(`{"resourceName":"people/c124"}`))
			require.NoError(t, err)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("rate-limit-contact-flow", "create-contact-step")
	input := CreateContactInput{GivenName: "Ada"}
	_, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	status = http.StatusServiceUnavailable
	ctx.AdvanceAttempt()
	_, err = sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.ErrorAs(t, err, &retry)
	status = http.StatusOK
	ctx.AdvanceAttempt()
	created, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchCreated, created.Branch)
	require.Equal(t, 3, requests)
}

func TestCreateContactResendsUnknownOutcomesUntilTheSendCap(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader([]int{http.StatusInternalServerError, http.StatusConflict, http.StatusBadGateway}[min(requests-1, 2)])
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("uncertain-contact-flow", "create-contact-step")
	input := CreateContactInput{GivenName: "Ada"}
	var retry *sdkgo.RetryError
	for range 2 {
		_, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
		require.ErrorAs(t, err, &retry)
		ctx.AdvanceAttempt()
	}
	uncertain, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchUncertain, uncertain.Branch)
	ctx.AdvanceAttempt()
	uncertain, err = sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchUncertain, uncertain.Branch)
	require.Equal(t, 3, requests)
}

func TestCreateContactSelectsUncertainOnTheFirstUnknownOutcomeWithSendCapOne(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusGatewayTimeout)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("cap-one-contact-flow", "create-contact-step")
	uncertain, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, CreateContactInput{GivenName: "Ada"}, sdkgo.WithMaxSends(1))
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, requests)
}

func TestCreateContactNeverResendsADefiniteRefusal(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusBadRequest)
		_, err := w.Write([]byte(`{"error":{"code":400,"status":"INVALID_ARGUMENT"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("rejected-contact-flow", "create-contact-step")
	rejected, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, CreateContactInput{GivenName: "Ada"})
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchProviderRejected, rejected.Branch)
	require.Equal(t, 1, requests)
	heartbeats := ctx.Heartbeats()
	require.Contains(t, string(heartbeats[len(heartbeats)-1]), `"state":"canResend"`)
}

func TestCreateContactSelectsUncertainForARefusalAfterAnUnknownOutcome(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader([]int{http.StatusInternalServerError, http.StatusForbidden}[min(requests-1, 1)])
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("refusal-after-unknown-contact-flow", "create-contact-step")
	input := CreateContactInput{GivenName: "Ada"}
	_, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.AdvanceAttempt()
	uncertain, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchUncertain, uncertain.Branch)
	require.Equal(t, 2, requests)
}

func TestCreateContactRetriesWhenTheCheckpointIsUnreadable(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests++ }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("fail-closed-contact-flow", "create-contact-step")
	ctx.SetReadError(errors.New("heartbeat store unavailable"))
	_, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, CreateContactInput{GivenName: "Ada"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	require.Zero(t, requests)
}

func TestCreateContactRejectsInvalidNameBeforeDispatch(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { requests++ }))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("invalid-contact-flow", "create-contact-step")
	result, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, CreateContactInput{GivenName: " "})
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchDefect, result.Branch)
	require.Zero(t, requests)
}
