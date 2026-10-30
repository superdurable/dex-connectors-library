// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package unsplash

import (
	"context"
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

func (providerContext) FlowID() string              { return "unsplash-provider-test" }
func (providerContext) StepExecutionID() string     { return "provider-step" }
func (providerContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (providerContext) Done() <-chan struct{}       { return nil }
func (providerContext) Err() error                  { return nil }
func (providerContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "unsplash", Name: "unsplash"}

func newProviderClient(t *testing.T, address string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: address}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessKey: sdkgo.NewSecretString("unsplash-test-key")}})
	require.NoError(t, err)
	return client
}

const photoJSON = `{"id":"office-1","description":"A sunlit office","alt_description":"sunlit office interior","urls":{"regular":"https://images.unsplash.com/photo-office-1"},"links":{"html":"https://unsplash.com/photos/office-1"},"user":{"name":"A. Photographer","links":{"html":"https://unsplash.com/@photographer"}}}`

func TestSearchPhotosUsesHeaderAndDecodesPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/search/photos", r.URL.Path)
		require.Equal(t, "office", r.URL.Query().Get("query"))
		require.Equal(t, "5", r.URL.Query().Get("per_page"))
		require.Equal(t, "Client-ID unsplash-test-key", r.Header.Get("Authorization"))
		require.NotContains(t, r.URL.String(), "unsplash-test-key")
		_, err := fmt.Fprintf(w, `{"total":1,"total_pages":1,"results":[%s]}`, photoJSON)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(providerContext{}, newProviderClient(t, server.URL).SearchPhotos(), testConnection, SearchPhotosInput{Query: "office", PerPage: 5})
	require.NoError(t, err)
	require.Equal(t, SearchPhotosBranchSearched, result.Branch)
	require.Equal(t, 1, result.Value.Total)
	require.Equal(t, "office-1", result.Value.Photos[0].ID)
	require.Equal(t, "https://images.unsplash.com/photo-office-1", result.Value.Photos[0].RegularURL)
}

func TestSearchPhotosRejectsBlankQueryBeforeDispatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected provider request") }))
	defer server.Close()
	result, err := sdkgo.RunQuery(providerContext{}, newProviderClient(t, server.URL).SearchPhotos(), testConnection, SearchPhotosInput{})
	require.NoError(t, err)
	require.Equal(t, SearchPhotosBranchDefect, result.Branch)
}

func TestGetPhotoAndNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/photos/office-1", r.URL.Path)
		_, err := fmt.Fprint(w, photoJSON)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(providerContext{}, newProviderClient(t, server.URL).GetPhoto(), testConnection, GetPhotoInput{ID: "office-1"})
	require.NoError(t, err)
	require.Equal(t, GetPhotoBranchFound, result.Branch)
	require.Equal(t, "A. Photographer", result.Value.PhotographerName)

	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer missing.Close()
	result, err = sdkgo.RunQuery(providerContext{}, newProviderClient(t, missing.URL).GetPhoto(), testConnection, GetPhotoInput{ID: "missing"})
	require.NoError(t, err)
	require.Equal(t, GetPhotoBranchNotFound, result.Branch)
}
