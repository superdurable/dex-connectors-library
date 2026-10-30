// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package algolia

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type providerContext struct{ dex.Context }

func (providerContext) FlowID() string              { return "algolia-provider-test" }
func (providerContext) StepExecutionID() string     { return "provider-step" }
func (providerContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (providerContext) Done() <-chan struct{}       { return nil }
func (providerContext) Err() error                  { return nil }
func (providerContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "algolia", Name: "algolia"}

func newProviderClient(t *testing.T, address string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: address + "/1"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {ApplicationID: "TESTAPP", APIKey: sdkgo.NewSecretString("algolia-test-key")}})
	require.NoError(t, err)
	return client
}

func TestSearchUsesHeadersAndDecodesHits(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/1/indexes/products/query", r.URL.Path)
		require.Equal(t, "TESTAPP", r.Header.Get("x-algolia-application-id"))
		require.Equal(t, "algolia-test-key", r.Header.Get("x-algolia-api-key"))
		require.NotContains(t, r.URL.String(), "algolia-test-key")
		var payload map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Contains(t, payload["params"], "query=notebook")
		_, err := fmt.Fprint(w, `{"nbHits":1,"hits":[{"objectID":"product-1","name":"Meridian notebook"}]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(providerContext{}, newProviderClient(t, server.URL).Search(), testConnection, SearchInput{IndexName: "products", Query: "notebook"})
	require.NoError(t, err)
	require.Equal(t, SearchBranchSearched, result.Branch)
	require.Equal(t, "product-1", result.Value.Hits[0].ObjectID)
}

func TestSearchRejectsBadIndexBeforeDispatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected provider request") }))
	defer server.Close()
	result, err := sdkgo.RunQuery(providerContext{}, newProviderClient(t, server.URL).Search(), testConnection, SearchInput{IndexName: "../bad"})
	require.NoError(t, err)
	require.Equal(t, SearchBranchDefect, result.Branch)
}

func TestSaveObjectReplacesByStableID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "PUT", r.Method)
		require.Equal(t, "/1/indexes/products/product-1", r.URL.Path)
		var payload map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, "product-1", payload["objectID"])
		require.Equal(t, "Meridian notebook", payload["name"])
		_, err := fmt.Fprint(w, `{"objectID":"product-1","taskID":1514562690001}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(providerContext{}, newProviderClient(t, server.URL).SaveObject(), testConnection, SaveObjectInput{IndexName: "products", ObjectID: "product-1", Attributes: map[string]string{"name": "Meridian notebook"}})
	require.NoError(t, err)
	require.Equal(t, SaveObjectBranchSaved, result.Branch)
	require.Equal(t, SavedObject{ObjectID: "product-1", TaskID: 1514562690001}, result.Value)
}

func TestSaveObjectRejectsReservedObjectIDBeforeDispatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected provider request") }))
	defer server.Close()
	result, err := sdkgo.RunMutation(providerContext{}, newProviderClient(t, server.URL).SaveObject(), testConnection, SaveObjectInput{IndexName: "products", ObjectID: "product-1", Attributes: map[string]string{"objectID": "other"}})
	require.NoError(t, err)
	require.Equal(t, SaveObjectBranchDefect, result.Branch)
}
