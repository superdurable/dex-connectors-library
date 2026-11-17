// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package odoo

import (
	"encoding/json"
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
		_, _ = writer.Write([]byte(`[{"id":42,"name":"Same","email":"same@example.com","phone":"123"}]`))
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	for _, scenario := range []struct {
		name   string
		invoke func(sdkgo.Call)
	}{
		{name: "contact with identical name email and phone", invoke: func(call sdkgo.Call) {
			actual := client.CreateContact().Invoke(call, CreateContactInput{Name: "Same", Email: "same@example.com", Phone: "123"})
			expected := sdkgo.NewMutationUncertain(CreatedContact{}, contactFailure("createContact", sdkgo.FailureTransport, "contact creation may have succeeded"), sdkgo.Receipt{})
			require.Equal(t, expected, actual)
		}},
		{name: "opportunity with identical name email and phone", invoke: func(call sdkgo.Call) {
			actual := client.CreateOpportunity().Invoke(call, CreateOpportunityInput{Name: "Same", EmailFrom: "same@example.com", Phone: "123"})
			expected := sdkgo.NewMutationUncertain(CreatedOpportunity{}, contactFailure("createOpportunity", sdkgo.FailureTransport, "opportunity creation may have succeeded"), sdkgo.Receipt{})
			require.Equal(t, expected, actual)
		}},
		{name: "record with identical selected field", invoke: func(call sdkgo.Call) {
			actual := client.CreateRecord().Invoke(call, CreateRecordInput{Model: "res.partner", Values: map[string]json.RawMessage{"name": json.RawMessage(`"Same"`)}, ReadBackField: "name"})
			expected := sdkgo.NewMutationUncertain(CreatedRecord{}, contactFailure("createRecord", sdkgo.FailureTransport, "record creation may have succeeded"), sdkgo.Receipt{})
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
