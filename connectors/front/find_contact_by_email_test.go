// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package front_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/front"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestFindContactByEmailUsesTheEmailAlias(t *testing.T) {
	provider := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":"crd_1y8sp71","name":"Jane Smith","description":"VIP","is_private":false,"updated_at":1767225600,
			"handles":[{"handle":"Jane@Acme.example.com","source":"email"},{"handle":"+12345678900","source":"phone"}],"lists":[{"name":"Customers"}]}`)
	})
	result, err := sdkgo.RunQuery(newTestDexContext("contact"), newFrontClient(t, provider.URL).FindContactByEmail(), frontConnection,
		front.FindContactByEmailInput{Email: "jane@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, front.FindContactByEmailBranchFound, result.Branch)
	require.Equal(t, "/contacts/alt:email:jane@acme.example.com", provider.request(0).path)
	require.Len(t, result.Value.Contacts, 1)
	contact := result.Value.Contacts[0]
	require.Equal(t, "crd_1y8sp71", contact.ID)
	require.Equal(t, []string{"Customers"}, contact.ListNames)
	require.Len(t, contact.Handles, 2)
	require.Equal(t, "crd_1y8sp71", result.Receipt.ProviderObjectID)
}

func TestFindContactByEmailMapsNotFoundAndAForeignContact(t *testing.T) {
	missing := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeFrontError(t, response, http.StatusNotFound)
	})
	result, err := sdkgo.RunQuery(newTestDexContext("contact-missing"), newFrontClient(t, missing.URL).FindContactByEmail(), frontConnection,
		front.FindContactByEmailInput{Email: "nobody@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, front.FindContactByEmailBranchNotFound, result.Branch)
	require.Empty(t, result.Value.Contacts)
	require.NotNil(t, result.Value.Contacts)

	foreign := newRecordingFront(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"id":"crd_9","handles":[{"handle":"ben@meridian.example.com","source":"email"}]}`)
	})
	result, err = sdkgo.RunQuery(newTestDexContext("contact-foreign"), newFrontClient(t, foreign.URL).FindContactByEmail(), frontConnection,
		front.FindContactByEmailInput{Email: "jane@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, front.FindContactByEmailBranchInvalidResponse, result.Branch, "a contact without the requested handle is not a match")

	for _, email := range []string{"", "Jane <jane@acme.example.com>", "jane@acme.example.com/../teammates", "not-an-address"} {
		result, err = sdkgo.RunQuery(newTestDexContext("contact-invalid"), newFrontClient(t, foreign.URL).FindContactByEmail(), frontConnection,
			front.FindContactByEmailInput{Email: email})
		require.NoError(t, err)
		require.Equal(t, front.FindContactByEmailBranchDefect, result.Branch, email)
	}
	require.Equal(t, 1, foreign.requestCount())
}
