// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package drip

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestDeleteSubscriberTreatsAlreadyAbsentAsSuccess(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodDelete, request.Method)
		require.Equal(t, "/v2/9999999/subscribers/ada@example.com", request.URL.Path)
		username, password, ok := request.BasicAuth()
		require.True(t, ok)
		require.Equal(t, testAPIKey, username)
		require.Empty(t, password)
		writer.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v2")
	result, err := sdkgo.RunMutation(queryContext{}, client.DeleteSubscriber(), testConnection, DeleteSubscriberInput{AccountID: "9999999", SubscriberIDOrEmail: "ada@example.com"})
	require.NoError(t, err)
	require.Equal(t, DeleteSubscriberBranchDeleted, result.Branch)
	require.True(t, result.Value.Deleted)
}

func TestUpsertSubscriberSendsOnlyDeclaredIdentityFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v2/9999999/subscribers", request.URL.Path)
		require.Equal(t, "application/json", request.Header.Get("Content-Type"))
		var body struct {
			Subscribers []map[string]any `json:"subscribers"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, []map[string]any{{"email": "ada@example.com", "first_name": "Ada", "last_name": "Lovelace"}}, body.Subscribers)
		_, err := writer.Write([]byte(`{"subscribers":[{"id":"subscriber-1","email":"ada@example.com","first_name":"Ada","last_name":"Lovelace","status":"active","custom_fields":{"private":"not-persisted"}}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v2")
	result, err := sdkgo.RunMutation(queryContext{}, client.UpsertSubscriber(), testConnection, UpsertSubscriberInput{AccountID: "9999999", Email: "ada@example.com", FirstName: "Ada", LastName: "Lovelace"})
	require.NoError(t, err)
	require.Equal(t, UpsertSubscriberBranchUpserted, result.Branch)
	require.Equal(t, "subscriber-1", result.Value.ID)
}
