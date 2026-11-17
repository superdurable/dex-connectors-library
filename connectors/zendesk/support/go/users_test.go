// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support_test

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestUserQueriesReadProviderPages(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch request.URL.Path {
		case "/api/v2/users":
			require.Equal(t, "2", request.URL.Query().Get("page[size]"))
			writeJSON(t, response, http.StatusOK, `{"users":[{"id":7701,"name":"Jane Smith","email":"jane@acme.example.com","role":"end-user","notes":"SENTINEL"}],"meta":{"has_more":true,"after_cursor":"next"}}`)
		case "/api/v2/users/7701":
			writeJSON(t, response, http.StatusOK, `{"user":{"id":7701,"name":"Jane Smith","email":"jane@acme.example.com","role":"end-user"}}`)
		case "/api/v2/users/search":
			require.Equal(t, "jane", request.URL.Query().Get("query"))
			writeJSON(t, response, http.StatusOK, `{"users":[{"id":7701,"name":"Jane Smith","email":"jane@acme.example.com","role":"end-user"}],"next_page":null}`)
		default:
			t.Fatalf("unexpected path %s", request.URL.Path)
		}
	})
	client := newZendeskClient(t, provider.URL)
	listed, err := sdkgo.RunQuery(newZendeskDexContext("list-users"), client.ListUsers(), zendeskConnection, support.ListUsersInput{PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, "next", listed.Value.NextCursor)
	require.Len(t, listed.Value.Users, 1)
	found, err := sdkgo.RunQuery(newZendeskDexContext("get-user"), client.GetUser(), zendeskConnection, support.GetUserInput{UserID: 7701})
	require.NoError(t, err)
	require.Equal(t, "Jane Smith", found.Value.User.Name)
	searched, err := sdkgo.RunQuery(newZendeskDexContext("search-users"), client.SearchUsers(), zendeskConnection, support.SearchUsersInput{Query: "jane", Page: 1, PageSize: 2})
	require.NoError(t, err)
	require.Len(t, searched.Value.Users, 1)
}

func TestUserWritesClassifyProviderOutcomes(t *testing.T) {
	name := "Jane Smith"
	for _, status := range []int{http.StatusCreated, http.StatusTooManyRequests, http.StatusBadGateway, http.StatusUnprocessableEntity} {
		provider := newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
			require.Equal(t, http.MethodPost, request.Method)
			require.Equal(t, "/api/v2/users", request.URL.Path)
			if status == http.StatusCreated {
				writeJSON(t, response, status, `{"user":{"id":7701,"name":"Jane Smith","email":"jane@acme.example.com","external_id":"dex-jane-1","role":"end-user"}}`)
				return
			}
			response.WriteHeader(status)
		})
		result, err := sdkgo.RunMutation(newZendeskDexContext("create-user"), newZendeskClient(t, provider.URL).CreateUser(), zendeskConnection, support.CreateUserInput{Name: name, Email: "jane@acme.example.com", ExternalID: "dex-jane-1"})
		if status == http.StatusCreated {
			require.NoError(t, err)
			require.Equal(t, support.CreateUserBranchCreated, result.Branch)
		} else if status == http.StatusUnprocessableEntity {
			require.NoError(t, err)
			require.Equal(t, support.CreateUserBranchProviderRejected, result.Branch)
		} else {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		}
	}
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch request.Method {
		case http.MethodPut:
			writeJSON(t, response, http.StatusOK, `{"user":{"id":7701,"name":"Jane Smith","email":"jane@acme.example.com"}}`)
		case http.MethodDelete:
			writeJSON(t, response, http.StatusOK, `{"user":{"id":7701,"name":"Jane Smith","active":false}}`)
		default:
			t.Fatalf("unexpected method %s", request.Method)
		}
	})
	client := newZendeskClient(t, provider.URL)
	updated, err := sdkgo.RunMutation(newZendeskDexContext("update-user"), client.UpdateUser(), zendeskConnection, support.UpdateUserInput{UserID: 7701, Name: &name})
	require.NoError(t, err)
	require.Equal(t, support.UpdateUserBranchUpdated, updated.Branch)
	deleted, err := sdkgo.RunMutation(newZendeskDexContext("delete-user"), client.DeleteUser(), zendeskConnection, support.DeleteUserInput{UserID: 7701})
	require.NoError(t, err)
	require.Equal(t, support.DeleteUserBranchDeleted, deleted.Branch)
}

func TestCreateUserDerivesStableExternalIDWhenBlank(t *testing.T) {
	var provider *recordingZendesk
	provider = newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, index int) {
		var envelope struct {
			User struct {
				Name       string `json:"name"`
				Email      string `json:"email"`
				ExternalID string `json:"external_id"`
			} `json:"user"`
		}
		require.NoError(t, json.Unmarshal([]byte(provider.request(index).body), &envelope))
		require.True(t, strings.HasPrefix(envelope.User.ExternalID, "dex-"))
		require.NotEqual(t, "dex-", envelope.User.ExternalID)
		writeJSON(t, response, http.StatusCreated, `{"user":{"id":7701,"name":"Jane Smith","email":"jane@acme.example.com","external_id":"`+envelope.User.ExternalID+`"}}`)
	})
	result, err := sdkgo.RunMutation(newZendeskDexContext("create-user-derived-id"), newZendeskClient(t, provider.URL).CreateUser(), zendeskConnection, support.CreateUserInput{Name: "Jane Smith", Email: "jane@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, support.CreateUserBranchCreated, result.Branch)
	require.True(t, strings.HasPrefix(result.Value.User.ExternalID, "dex-"))
}
