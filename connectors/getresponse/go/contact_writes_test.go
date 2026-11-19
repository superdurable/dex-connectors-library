// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package getresponse

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestCreateContactQueuesOneRequest(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		calls++
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v3/contacts", request.URL.Path)
		require.Equal(t, "api-key "+testAPIKey, request.Header.Get("X-Auth-Token"))
		writer.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v3")
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("create-contact-flow", "create-contact-step"), client.CreateContact(), testConnection, CreateContactInput{Email: "ada@example.com", CampaignID: "abc123", Name: "Ada"})
	require.NoError(t, err)
	require.Equal(t, CreateContactBranchAccepted, result.Branch)
	require.Equal(t, 1, calls)
}

func TestUpdateAndDeleteContactClassifyProviderResponses(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "/v3/contacts/abc123", request.URL.Path)
		if request.Method == http.MethodPost {
			writer.Header().Set("Content-Type", "application/json")
			_, err := writer.Write([]byte(`{"contactId":"abc123","email":"ada@example.com","name":"Ada Byron"}`))
			require.NoError(t, err)
			return
		}
		require.Equal(t, http.MethodDelete, request.Method)
		writer.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/v3")
	context := singledispatchtest.NewContext("contact-flow", "update-step")
	updated, err := sdkgo.RunMutation(context, client.UpdateContact(), testConnection, UpdateContactInput{ContactID: "abc123", Name: "Ada Byron"})
	require.NoError(t, err)
	require.Equal(t, UpdateContactBranchUpdated, updated.Branch)
	require.Equal(t, "Ada Byron", updated.Value.Name)
	deleted, err := sdkgo.RunMutation(singledispatchtest.NewContext("contact-flow", "delete-step"), client.DeleteContact(), testConnection, DeleteContactInput{ContactID: "abc123"})
	require.NoError(t, err)
	require.Equal(t, DeleteContactBranchDeleted, deleted.Branch)
}
