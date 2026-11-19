// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestDealNoteQueriesFilterRelationships(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/3/notes":
			require.Equal(t, "Deal", r.URL.Query().Get("filters[reltype]"))
			require.Equal(t, "46", r.URL.Query().Get("filters[relid]"))
			_, err := w.Write([]byte(`{"notes":[{"id":"91","relid":"46","reltype":"Deal","note":"Follow up","userid":"999"}],"meta":{"total":"1"}}`))
			require.NoError(t, err)
		case "/api/3/notes/91":
			_, err := w.Write([]byte(`{"note":{"id":"91","relid":"46","reltype":"Deal","note":"Follow up","userid":"999"}}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	page, err := sdkgo.RunQuery(queryContext{}, client.ListDealNotes(), testConnection, ListDealNotesInput{DealID: "46", Limit: 20})
	require.NoError(t, err)
	require.Equal(t, ListDealNotesBranchListed, page.Branch)
	require.Equal(t, 1, page.Value.Total)
	note, err := sdkgo.RunQuery(queryContext{}, client.GetDealNote(), testConnection, GetDealNoteInput{NoteID: "91"})
	require.NoError(t, err)
	require.Equal(t, GetDealNoteBranchFound, note.Branch)
	require.Equal(t, page.Value.Notes[0], note.Value)
}

func TestCreateDealNoteReadsBackAfterUnknownOutcome(t *testing.T) {
	var sends, reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			require.Equal(t, "/api/3/deals/46/notes", r.URL.Path)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"note":{"note":"Follow up"}}`, string(body))
			sends.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		case http.MethodGet:
			require.Equal(t, "/api/3/notes", r.URL.Path)
			require.Equal(t, "Deal", r.URL.Query().Get("filters[reltype]"))
			require.Equal(t, "46", r.URL.Query().Get("filters[relid]"))
			reads.Add(1)
			_, err := w.Write([]byte(`{"notes":[{"id":"91","relid":"46","reltype":"Deal","note":"Follow up"}],"meta":{"total":"1"}}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	ctx := &dealRetryContext{attempt: 1}
	input := CreateDealNoteInput{DealID: "46", Text: "Follow up"}
	_, err := sdkgo.RunMutation(ctx, client.CreateDealNote(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.attempt = 2
	created, err := sdkgo.RunMutation(ctx, client.CreateDealNote(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateDealNoteBranchCreated, created.Branch)
	require.Equal(t, "91", created.Value.ID)
	require.Equal(t, int32(1), sends.Load())
	require.Equal(t, int32(1), reads.Load())
}

func TestUpdateDealNoteReplacesText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPut, r.Method)
		require.Equal(t, "/api/3/deals/46/notes/91", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"note":{"note":"Follow up"}}`, string(body))
		_, err = w.Write([]byte(`{"note":{"id":"91","relid":"46","reltype":"Deal","note":"Follow up"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	result, err := sdkgo.RunMutation(queryContext{}, client.UpdateDealNote(), testConnection, UpdateDealNoteInput{DealID: "46", NoteID: "91", Text: "Follow up"})
	require.NoError(t, err)
	require.Equal(t, UpdateDealNoteBranchUpdated, result.Branch)
}
