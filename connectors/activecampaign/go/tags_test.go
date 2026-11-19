// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestTagQueriesFilterProviderFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, testAPIKey, r.Header.Get("Api-Token"))
		switch r.URL.Path {
		case "/api/3/tags":
			require.Equal(t, "3", r.URL.Query().Get("limit"))
			require.Equal(t, "2", r.URL.Query().Get("offset"))
			_, err := w.Write([]byte(`{"tags":[{"id":"16","tag":"Newsletter","tagType":"contact","description":"Monthly","links":{"contactGoalTags":"private-url"}}],"meta":{"total":"12"}}`))
			require.NoError(t, err)
		case "/api/3/tags/16":
			_, err := w.Write([]byte(`{"tag":{"id":"16","tag":"Newsletter","tagType":"contact","description":"Monthly","links":{"contactGoalTags":"private-url"}}}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	page, err := sdkgo.RunQuery(queryContext{}, client.ListTags(), testConnection, ListTagsInput{Offset: 2, Limit: 3})
	require.NoError(t, err)
	require.Equal(t, ListTagsBranchListed, page.Branch)
	require.Equal(t, 12, page.Value.Total)
	tag, err := sdkgo.RunQuery(queryContext{}, client.GetTag(), testConnection, GetTagInput{TagID: "16"})
	require.NoError(t, err)
	require.Equal(t, GetTagBranchFound, tag.Branch)
	require.Equal(t, page.Value.Tags[0], tag.Value)
	encoded, err := json.Marshal(tag.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-url")
}

func TestTagMutationsUseAbsoluteFieldsAndConvergeOnDeletion(t *testing.T) {
	var creates, updates, deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, testAPIKey, r.Header.Get("Api-Token"))
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/api/3/tags":
			creates.Add(1)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"tag":{"tag":"Newsletter","tagType":"contact","description":"Monthly"}}`, string(body))
			w.WriteHeader(http.StatusCreated)
			_, err = w.Write([]byte(`{"tag":{"id":"16","tag":"Newsletter","tagType":"contact","description":"Monthly"}}`))
			require.NoError(t, err)
		case r.Method == http.MethodPut && r.URL.Path == "/api/3/tags/16":
			updates.Add(1)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"tag":{"tag":"Newsletter","tagType":"contact","description":""}}`, string(body))
			_, err = w.Write([]byte(`{"tag":{"id":"16","tag":"Newsletter","tagType":"contact","description":""}}`))
			require.NoError(t, err)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/3/tags/16":
			if deletes.Add(1) == 1 {
				_, err := w.Write([]byte(`{}`))
				require.NoError(t, err)
			} else {
				w.WriteHeader(http.StatusNotFound)
				_, writeErr := w.Write([]byte(`{"errors":[{"title":"Not found"}]}`))
				require.NoError(t, writeErr)
			}
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	created, err := sdkgo.RunMutation(queryContext{}, client.CreateTag(), testConnection, CreateTagInput{Name: "Newsletter", Description: "Monthly"})
	require.NoError(t, err)
	require.Equal(t, CreateTagBranchCreated, created.Branch)
	require.Equal(t, "16", created.Value.ID)
	updated, err := sdkgo.RunMutation(queryContext{}, client.UpdateTag(), testConnection, UpdateTagInput{TagID: "16", Name: "Newsletter"})
	require.NoError(t, err)
	require.Equal(t, UpdateTagBranchUpdated, updated.Branch)
	for range 2 {
		deleted, err := sdkgo.RunMutation(queryContext{}, client.DeleteTag(), testConnection, DeleteTagInput{TagID: "16"})
		require.NoError(t, err)
		require.Equal(t, DeleteTagBranchDeleted, deleted.Branch)
		require.True(t, deleted.Value.Deleted)
	}
	require.Equal(t, int32(1), creates.Load())
	require.Equal(t, int32(1), updates.Load())
	require.Equal(t, int32(2), deletes.Load())
}

func TestCreateTagDoesNotTreatConflictAfterUnknownSendAsRejection(t *testing.T) {
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/api/3/tags", request.URL.Path)
		if sends.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusConflict)
		_, err := w.Write([]byte(`{"errors":[{"title":"Duplicate tag name"}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	ctx := &dealRetryContext{attempt: 1}
	input := CreateTagInput{Name: "Newsletter", Description: "Monthly"}
	for attempt := int32(1); attempt < 3; attempt++ {
		ctx.attempt = attempt
		_, err := sdkgo.RunMutation(ctx, client.CreateTag(), testConnection, input)
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
	}
	ctx.attempt = 3
	result, err := sdkgo.RunMutation(ctx, client.CreateTag(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateTagBranchUncertain, result.Branch)
	require.Equal(t, int32(3), sends.Load())
}

func TestCreateTagRetriesInconclusiveResponses(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "conflict", status: http.StatusConflict, body: `{"errors":[{"title":"Duplicate"}]}`},
		{name: "other client error", status: http.StatusMethodNotAllowed, body: `{"errors":[{"title":"Unsupported"}]}`},
		{name: "empty refusal", status: http.StatusBadRequest},
		{name: "contradictory refusal", status: http.StatusBadRequest, body: `{"errors":[{"title":"Rejected"}],"tag":{"id":"16"}}`},
		{name: "malformed success", status: http.StatusOK, body: `{"tag":{"id":"16"}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(testCase.status)
				_, err := w.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL+"/api/3")
			_, err := sdkgo.RunMutation(queryContext{}, client.CreateTag(), testConnection, CreateTagInput{Name: "Newsletter"})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		})
	}
}

func TestCreateTagFailsClosedToRetry(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("a malformed checkpoint must prevent sending")
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	ctx := &dealRetryContext{attempt: 2, heartbeat: []byte(`{invalid`)}
	_, err := sdkgo.RunMutation(ctx, client.CreateTag(), testConnection, CreateTagInput{Name: "Newsletter"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
