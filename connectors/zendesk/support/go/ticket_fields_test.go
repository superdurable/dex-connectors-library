// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package support_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestTicketFieldQueriesReadSafeMetadata(t *testing.T) {
	provider := newRecordingZendesk(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		require.Equal(t, http.MethodGet, request.Method)
		if request.URL.Path == "/api/v2/ticket_fields" {
			require.Equal(t, "2", request.URL.Query().Get("page[size]"))
			writeJSON(t, response, http.StatusOK, `{"ticket_fields":[{"id":34,"title":"Subject","type":"subject","active":true,"required":true,"description":"SENTINEL private"}],"meta":{"has_more":true,"after_cursor":"next"}}`)
			return
		}
		require.Equal(t, "/api/v2/ticket_fields/34", request.URL.Path)
		writeJSON(t, response, http.StatusOK, `{"ticket_field":{"id":34,"title":"Subject","type":"subject","active":true,"required":true,"description":"SENTINEL private"}}`)
	})
	client := newZendeskClient(t, provider.URL)
	listed, err := sdkgo.RunQuery(newZendeskDexContext("list-fields"), client.ListTicketFields(), zendeskConnection, support.ListTicketFieldsInput{PageSize: 2})
	require.NoError(t, err)
	require.Equal(t, "next", listed.Value.NextCursor)
	require.Len(t, listed.Value.Fields, 1)
	found, err := sdkgo.RunQuery(newZendeskDexContext("get-field"), client.GetTicketField(), zendeskConnection, support.GetTicketFieldInput{FieldID: 34})
	require.NoError(t, err)
	require.Equal(t, "Subject", found.Value.Field.Title)
	encoded, err := json.Marshal(found.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
}
