// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package reamaze_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/reamaze"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestFindContactByEmailKeepsOnlyExactMatchesFromTheNameOrEmailSearch(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, pageJSON("contacts", []any{
			map[string]any{"name": "Jane Smith", "email": "Jane@Acme.example.com", "friendly_name": "Jane", "notes": []any{map[string]any{"note": "SENTINEL"}}},
			map[string]any{"name": "Jane Lookalike", "email": "jane@acme.example.com.au"},
			map[string]any{"name": "jane@acme.example.com", "email": "other@example.com"},
		}, 1))
	})
	result, err := sdkgo.RunQuery(newReamazeDexContext("contact"), newReamazeClient(t, provider.URL).FindContactByEmail(), reamazeConnection,
		reamaze.FindContactByEmailInput{Email: "jane@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, reamaze.FindContactByEmailBranchFound, result.Branch)
	require.Equal(t, []reamaze.Contact{{Name: "Jane Smith", Email: "Jane@Acme.example.com", FriendlyName: "Jane"}}, result.Value.Contacts)
	require.False(t, result.Value.HasMoreCandidates)
	request := provider.request(0)
	require.Equal(t, "/api/v1/contacts", request.path)
	require.Equal(t, map[string][]string{"q": {"jane@acme.example.com"}, "type": {"email"}, "page": {"1"}}, request.query)
}

func TestFindContactByEmailSelectsNotFoundAndReportsMoreCandidates(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, pageJSON("contacts", []any{map[string]any{"name": "Jane Lookalike", "email": "jane@acme.example.com.au"}}, 2))
	})
	result, err := sdkgo.RunQuery(newReamazeDexContext("contact-none"), newReamazeClient(t, provider.URL).FindContactByEmail(), reamazeConnection,
		reamaze.FindContactByEmailInput{Email: "jane@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, reamaze.FindContactByEmailBranchNotFound, result.Branch)
	require.Empty(t, result.Value.Contacts)
	require.True(t, result.Value.HasMoreCandidates)
	require.Nil(t, result.Failure)
}

func TestFindContactByEmailAcceptsAnApostropheInTheLocalPart(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, pageJSON("contacts", []any{map[string]any{"name": "Sean O'Brien", "email": "sean.o'brien@example.ie"}}, 1))
	})
	result, err := sdkgo.RunQuery(newReamazeDexContext("contact-apostrophe"), newReamazeClient(t, provider.URL).FindContactByEmail(), reamazeConnection,
		reamaze.FindContactByEmailInput{Email: "sean.o'brien@example.ie"})
	require.NoError(t, err)
	require.Equal(t, reamaze.FindContactByEmailBranchFound, result.Branch)
	require.Equal(t, []string{"sean.o'brien@example.ie"}, provider.request(0).query["q"])
}

func TestFindContactByEmailRejectsAnInvalidAddressWithoutARequest(t *testing.T) {
	provider := newRecordingReamaze(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeValue(t, response, http.StatusOK, pageJSON("contacts", []any{}, 0))
	})
	for _, email := range []string{"", "jane", "Jane <jane@acme.example.com>", "jane@acme.example.com, ben@example.com"} {
		result, err := sdkgo.RunQuery(newReamazeDexContext("contact-invalid"), newReamazeClient(t, provider.URL).FindContactByEmail(), reamazeConnection,
			reamaze.FindContactByEmailInput{Email: email})
		require.NoError(t, err)
		require.Equal(t, reamaze.FindContactByEmailBranchDefect, result.Branch, email)
	}
	require.Zero(t, provider.requestCount())
}
