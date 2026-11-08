// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dropbox

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "dropbox", Name: "dropbox"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "dropbox-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func testCredentials() Credentials {
	return Credentials{OAuthClientID: "app-key", OAuthClientSecret: sdkgo.NewSecretString("app-secret"), AccessToken: sdkgo.NewSecretString("test-token"), RefreshToken: sdkgo.NewSecretString("refresh-token")}
}

func TestListFolderBearerAuthenticationAndPrivacy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/2/files/list_folder", r.URL.Path)
		require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
		require.Equal(t, "application/json", r.Header.Get("Content-Type"))
		var request struct {
			Path  string `json:"path"`
			Limit int    `json:"limit"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&request))
		require.Equal(t, "/Projects", request.Path)
		require.Equal(t, 20, request.Limit)
		_, err := w.Write([]byte(`{"entries":[{".tag":"file","id":"id:example","name":"example.txt","path_display":"/Projects/example.txt","content_hash":"not-persisted"}],"cursor":"next-cursor","has_more":true}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: testCredentials()})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListFolder(), testConnection, ListFolderInput{Path: "/Projects", Limit: 20})
	require.NoError(t, err)
	require.Equal(t, ListFolderBranchListed, result.Branch)
	require.Equal(t, []EntryMetadata{{ID: "id:example", Name: "example.txt", PathDisplay: "/Projects/example.txt", Kind: "file"}}, result.Value.Entries)
	require.Equal(t, "next-cursor", result.Value.Cursor)
	require.True(t, result.Value.HasMore)
}

func TestGetMetadataNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/2/files/get_metadata", r.URL.Path)
		w.WriteHeader(http.StatusConflict)
		_, err := w.Write([]byte(`{"error_summary":"path/not_found/...","error":{".tag":"path"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: testCredentials()})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetMetadata(), testConnection, GetMetadataInput{Path: "/Projects/missing.txt"})
	require.NoError(t, err)
	require.Equal(t, GetMetadataBranchNotFound, result.Branch)
}
