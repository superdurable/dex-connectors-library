// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package linear_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/linear"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func userJSON(email string) map[string]any {
	return map[string]any{
		"id": testUserID, "name": "Alice Nguyen", "displayName": "alice", "email": email, "active": true, "admin": false,
		"guest": false, "url": "https://linear.app/acme/profiles/alice",
	}
}

func TestFindUserByEmailComparesWithoutCase(t *testing.T) {
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, _ int) {
		writeData(t, response, map[string]any{"users": map[string]any{"nodes": []any{userJSON("alice@example.com")}}})
	})
	result, err := runQuery("find", newAPIKeyClient(t, provider.URL).FindUserByEmail(), linear.FindUserByEmailInput{Email: " Alice@Example.com "})
	require.NoError(t, err)
	require.Equal(t, linear.FindUserByEmailBranchFound, result.Branch)
	require.Equal(t, linear.User{
		ID: testUserID, Name: "Alice Nguyen", DisplayName: "alice", Email: "alice@example.com", IsActive: true,
		URL: "https://linear.app/acme/profiles/alice",
	}, result.Value)
	require.Equal(t, testUserID, result.Receipt.ProviderObjectID)
	request := provider.request(0)
	require.Equal(t, "LinearFindUserByEmail", request.operationName)
	require.Equal(t, map[string]any{"email": "Alice@Example.com", "includeDisabled": false}, request.variables)
}

func TestFindUserByEmailSelectsNotFoundAndRejectsAnotherUser(t *testing.T) {
	answers := []any{
		map[string]any{"users": map[string]any{"nodes": []any{}}},
		map[string]any{"users": map[string]any{"nodes": []any{userJSON("mallory@example.com")}}},
		map[string]any{"users": map[string]any{"nodes": []any{userJSON("alice@example.com"), userJSON("alice@example.com")}}},
	}
	provider := newRecordingLinear(t, func(response http.ResponseWriter, _ recordedRequest, index int) {
		writeData(t, response, answers[index])
	})
	client := newAPIKeyClient(t, provider.URL)
	missing, err := runQuery("missing", client.FindUserByEmail(), linear.FindUserByEmailInput{Email: "alice@example.com", IncludesDisabled: true})
	require.NoError(t, err)
	require.Equal(t, linear.FindUserByEmailBranchNotFound, missing.Branch)
	require.Equal(t, sdkgo.FailureNotFound, missing.Failure.Kind)
	require.Equal(t, true, provider.request(0).variables["includeDisabled"])
	for _, step := range []string{"other", "two"} {
		result, err := runQuery(step, client.FindUserByEmail(), linear.FindUserByEmailInput{Email: "alice@example.com"})
		require.NoError(t, err)
		require.Equal(t, linear.FindUserByEmailBranchInvalidResponse, result.Branch, step)
	}
}

func TestFindUserByEmailRejectsAnAddressWithoutARequest(t *testing.T) {
	provider := newRecordingLinear(t, func(http.ResponseWriter, recordedRequest, int) { t.Error("no request may be sent") })
	for _, email := range []string{"", "alice", "alice@", "alice@example", "a b@example.com", "a@b@example.com"} {
		result, err := runQuery("bad-"+email, newAPIKeyClient(t, provider.URL).FindUserByEmail(), linear.FindUserByEmailInput{Email: email})
		require.NoError(t, err)
		require.Equal(t, linear.FindUserByEmailBranchDefect, result.Branch, email)
	}
}
