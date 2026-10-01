// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package intercom_test

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/intercom"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestFindContactByEmailReturnsEveryMatchingContact(t *testing.T) {
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"type":"list","total_count":3,"data":[
			{"type":"contact","id":"5ba682d23d7cf92bef87bfd4","role":"user","email":"jane@acme.example.com","name":"Jane Smith","external_id":"cont_010","created_at":1767225600,"updated_at":1767229200,"owner_id":"321","custom_attributes":{"plan":"pro"}},
			{"type":"contact","id":"6a3b2c4d5e6f7a8b9c0d1e2f","role":"lead","email":"jane@acme.example.com","name":null,"external_id":null,"created_at":1767139200,"updated_at":1767139200}
		],"pages":{"type":"pages","page":1,"per_page":2,"total_pages":2,"next":{"per_page":2,"starting_after":"WzE3Njcx"}}}`)
	})
	result, err := sdkgo.RunQuery(newTestDexContext("find"), newIntercomClient(t, provider.URL).FindContactByEmail(), intercomConnection,
		intercom.FindContactByEmailInput{Email: "jane@acme.example.com", ContactLimit: 2})
	require.NoError(t, err)
	require.Equal(t, intercom.FindContactByEmailBranchFound, result.Branch)
	require.Len(t, result.Value.Contacts, 2)
	require.True(t, result.Value.HasMoreContacts)
	user, lead := result.Value.Contacts[0], result.Value.Contacts[1]
	require.Equal(t, "5ba682d23d7cf92bef87bfd4", user.ID)
	require.Equal(t, "user", user.Role)
	require.Equal(t, "cont_010", user.ExternalID)
	require.Equal(t, int64(1767225600), user.CreatedAt.Unix())
	require.Equal(t, "lead", lead.Role)
	require.Empty(t, lead.Name)
	require.JSONEq(t, `{"query":{"field":"email","operator":"=","value":"jane@acme.example.com"},"pagination":{"per_page":2}}`, provider.request(0).body)
	require.Equal(t, "/contacts/search", provider.request(0).path)
}

func TestFindContactByEmailSelectsNotFoundForNoContact(t *testing.T) {
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"type":"list","total_count":0,"data":[],"pages":{"type":"pages","page":1}}`)
	})
	result, err := sdkgo.RunQuery(newTestDexContext("find-none"), newIntercomClient(t, provider.URL).FindContactByEmail(), intercomConnection,
		intercom.FindContactByEmailInput{Email: "nobody@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, intercom.FindContactByEmailBranchNotFound, result.Branch)
	require.Empty(t, result.Value.Contacts)
	require.Contains(t, provider.request(0).body, `"per_page":10`)
}

func TestFindContactByEmailRejectsInvalidInputAndPages(t *testing.T) {
	provider := newRecordingIntercom(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"type":"list","data":[{"type":"contact","id":"bad id","role":"user"}]}`)
	})
	client := newIntercomClient(t, provider.URL)
	for name, input := range map[string]intercom.FindContactByEmailInput{
		"display address": {Email: "Jane <jane@acme.example.com>"},
		"two addresses":   {Email: "jane@acme.example.com,ben@acme.example.com"},
		"too many":        {Email: "jane@acme.example.com", ContactLimit: intercom.MaxContactLimit + 1},
	} {
		result, err := sdkgo.RunQuery(newTestDexContext("find-invalid-"+name), client.FindContactByEmail(), intercomConnection, input)
		require.NoError(t, err)
		require.Equal(t, intercom.FindContactByEmailBranchDefect, result.Branch, name)
	}
	require.Zero(t, provider.requestCount())
	result, err := sdkgo.RunQuery(newTestDexContext("find-invalid-page"), client.FindContactByEmail(), intercomConnection, intercom.FindContactByEmailInput{Email: "jane@acme.example.com"})
	require.NoError(t, err)
	require.Equal(t, intercom.FindContactByEmailBranchInvalidResponse, result.Branch)
}
