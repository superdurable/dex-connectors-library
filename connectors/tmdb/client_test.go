// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package tmdb

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type providerTestContext struct{ dex.Context }

func (providerTestContext) FlowID() string              { return "provider-test" }
func (providerTestContext) StepExecutionID() string     { return "provider-step" }
func (providerTestContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (providerTestContext) Done() <-chan struct{}       { return nil }
func (providerTestContext) Err() error                  { return nil }
func (providerTestContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "tmdb", Name: "tmdb"}

func TestSearchMoviesHTTPAndFailure(t *testing.T) {
	reject := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "GET", request.Method)
		require.Equal(t, "/search/movie?page=1&query=Jack+Reacher", request.URL.RequestURI())
		require.Equal(t, "Bearer test-secret-token", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		require.NotContains(t, request.URL.String(), "test-secret-token")

		if reject {
			writer.WriteHeader(400)
			return
		}
		_, err := io.WriteString(writer, `{"page":1,"total_pages":1,"total_results":1,"results":[{"id":343611,"title":"Jack Reacher","overview":"A thriller.","release_date":"2016-10-19"}]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(providerTestContext{}, client.SearchMovies(), testConnection, SearchMoviesInput{Query: "Jack Reacher", Page: 1})
	require.NoError(t, err)
	require.Equal(t, SearchMoviesBranchSearched, result.Branch)
	require.Nil(t, result.Failure)
	reject = true
	rejected, err := sdkgo.RunQuery(providerTestContext{}, client.SearchMovies(), testConnection, SearchMoviesInput{Query: "Jack Reacher", Page: 1})
	require.NoError(t, err)
	require.Equal(t, SearchMoviesBranchProviderRejected, rejected.Branch)
	require.NotNil(t, rejected.Failure)
}

func TestGetMovieHTTPAndFailure(t *testing.T) {
	reject := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "GET", request.Method)
		require.Equal(t, "/movie/343611", request.URL.RequestURI())
		require.Equal(t, "Bearer test-secret-token", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		require.NotContains(t, request.URL.String(), "test-secret-token")

		if reject {
			writer.WriteHeader(404)
			return
		}
		_, err := io.WriteString(writer, `{"id":343611,"title":"Jack Reacher","overview":"A thriller.","release_date":"2016-10-19"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(providerTestContext{}, client.GetMovie(), testConnection, GetMovieInput{ID: 343611})
	require.NoError(t, err)
	require.Equal(t, GetMovieBranchFound, result.Branch)
	require.Nil(t, result.Failure)
	reject = true
	rejected, err := sdkgo.RunQuery(providerTestContext{}, client.GetMovie(), testConnection, GetMovieInput{ID: 343611})
	require.NoError(t, err)
	require.Equal(t, GetMovieBranchNotFound, rejected.Branch)
	require.NotNil(t, rejected.Failure)
}
