// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contacts

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestCreateContactConfirmsOneSend(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/v1/people:createContact", r.URL.Path)
		require.Equal(t, "names,emailAddresses", r.URL.Query().Get("personFields"))
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
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
	uncertain, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, requests)
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

func TestCreateContactDoesNotRepeatAmbiguousFailure(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	ctx := singledispatchtest.NewContext("uncertain-contact-flow", "create-contact-step")
	input := CreateContactInput{GivenName: "Ada"}
	uncertain, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchUncertain, uncertain.Branch)
	ctx.AdvanceAttempt()
	uncertain, err = sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchUncertain, uncertain.Branch)
	require.Equal(t, 1, requests)
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
