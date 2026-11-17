// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package productboard

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const testNoteID = "323e4567-e89b-12d3-a456-426614174000"

func TestNoteQueriesExcludeContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "Bearer sample-productboard-token", request.Header.Get("Authorization"))
		if request.URL.Path == "/notes" {
			require.Equal(t, "name", request.URL.Query().Get("fields[]"))
			_, err := writer.Write([]byte(`{"data":[{"id":"` + testNoteID + `","type":"textNote","fields":{"name":"Feedback","content":"private feedback"}}],"links":{"next":""}}`))
			require.NoError(t, err)
			return
		}
		require.Equal(t, "/notes/"+testNoteID, request.URL.Path)
		require.Equal(t, "name", request.URL.Query().Get("fields"))
		_, err := writer.Write([]byte(`{"data":{"id":"` + testNoteID + `","type":"textNote","fields":{"name":"Feedback","content":"private feedback"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	listed, err := sdkgo.RunQuery(queryContext{}, client.ListNotes(), testConnection, ListNotesInput{PageCursor: "cursor-1"})
	require.NoError(t, err)
	require.Equal(t, ListNotesBranchListed, listed.Branch)
	require.Equal(t, NoteMetadata{ID: testNoteID, Type: "textNote", Name: "Feedback"}, listed.Value.Notes[0])
	found, err := sdkgo.RunQuery(queryContext{}, client.GetNote(), testConnection, GetNoteInput{NoteID: testNoteID})
	require.NoError(t, err)
	require.Equal(t, GetNoteBranchFound, found.Branch)
	encoded, err := json.Marshal(found.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private feedback")
}

func TestCreateTextNoteReturnsOnlyMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		var payload struct {
			Data struct {
				Type   string `json:"type"`
				Fields struct {
					Name    string `json:"name"`
					Content string `json:"content"`
				} `json:"fields"`
			} `json:"data"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&payload))
		require.Equal(t, "textNote", payload.Data.Type)
		require.Equal(t, "<p>private feedback</p>", payload.Data.Fields.Content)
		writer.WriteHeader(http.StatusCreated)
		_, err := writer.Write([]byte(`{"data":{"id":"` + testNoteID + `","type":"textNote"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(singledispatchtest.NewContext("productboard-note", "create-note"), newTestClient(t, server.URL, 0).CreateTextNote(), testConnection, CreateTextNoteInput{Name: "Feedback", Content: "<p>private feedback</p>"})
	require.NoError(t, err)
	require.Equal(t, CreateTextNoteBranchCreated, result.Branch)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private feedback")
}
