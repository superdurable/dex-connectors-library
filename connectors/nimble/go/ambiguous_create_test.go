// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package nimble

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestAmbiguousCreatesDoNotConfirmPreexistingMatches(t *testing.T) {
	serverRequests := 0
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		serverRequests++
		if request.URL.Path == "/api/v1/contacts" {
			_, _ = writer.Write([]byte(`{"resources":[{"id":"5049fb849b85f669e40000dc","fields":{"email":[{"value":"same@example.com"}],"first name":[{"value":"Same"}]}}]}`))
			return
		}
		_, _ = writer.Write([]byte(`{"resources":[{"id":"5049fb849b85f669e40000dc","note":"same note","note_preview":"same preview"}]}`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/v1")
	for _, scenario := range []struct {
		name   string
		invoke func(sdkgo.Call)
	}{
		{name: "contact with identical name and email", invoke: func(call sdkgo.Call) {
			actual := client.CreateContact().Invoke(call, CreateContactInput{FirstName: "Same", EmailAddress: "same@example.com"})
			expected := sdkgo.NewMutationUncertain(ContactMetadata{}, queryFailure("createContact", sdkgo.FailureTransport, "an earlier contact create may have succeeded"), sdkgo.Receipt{})
			require.Equal(t, expected, actual)
		}},
		{name: "note with identical text and preview", invoke: func(call sdkgo.Call) {
			actual := client.CreateContactNote().Invoke(call, CreateContactNoteInput{ContactIDs: []string{testContactID}, Note: "same note", NotePreview: "same preview"})
			expected := sdkgo.NewMutationUncertain(ContactNote{}, queryFailure("createContactNote", sdkgo.FailureTransport, "an earlier note create may have succeeded"), sdkgo.Receipt{})
			require.Equal(t, expected, actual)
		}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx := singledispatchtest.NewContext("ambiguous-flow", "create")
			ctx.AdvanceAttempt()
			require.NoError(t, ctx.SetPersistedCheckpoint(singledispatch.Marker{CallID: "ambiguous-create", Attempt: 1, State: singledispatch.StateClaimed, SendCount: 1}))
			scenario.invoke(sdkgo.Call{Context: ctx, ID: "ambiguous-create", Connection: testConnection})
		})
	}
	require.Zero(t, serverRequests, "a matching preexisting resource must not confirm this request")
}
