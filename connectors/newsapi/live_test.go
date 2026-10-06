//go:build live

// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package newsapi

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// TestLiveSearchArticles makes one rejected request with a wrong key and one small real search.
func TestLiveSearchArticles(t *testing.T) {
	apiKey := os.Getenv("NEWSAPI_CONNECTOR_TEST_API_KEY")
	if apiKey == "" {
		t.Skip("NEWSAPI_CONNECTOR_TEST_API_KEY is not configured")
	}
	requireNoAPIKey := func(t *testing.T, value any) {
		t.Helper()
		encoded, err := json.Marshal(value)
		require.NoError(t, err)
		// A boolean assertion keeps the key out of the failure message.
		require.False(t, strings.Contains(string(encoded), apiKey), "a Result contains the API key")
	}

	t.Run("a wrong key is an authentication rejection", func(t *testing.T) {
		client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{
			testConnection: {APIKey: sdkgo.NewSecretString("00000000000000000000000000000000")},
		})
		require.NoError(t, err)
		rejected, err := searchArticles(t, client, SearchArticlesInput{Query: "technology", PageSize: 1})
		require.NoError(t, err)
		require.Equal(t, SearchArticlesBranchProviderRejected, rejected.Branch)
		require.Equal(t, sdkgo.FailureAuthentication, rejected.Failure.Kind, rejected.Failure.Message)
		t.Logf("branch=%s failure=%q", rejected.Branch, rejected.Failure.Message)
	})

	t.Run("a title search returns typed articles", func(t *testing.T) {
		client, err := New(Config{}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString(apiKey)}})
		require.NoError(t, err)
		searched, err := searchArticles(t, client, SearchArticlesInput{
			Query: "technology", SearchIn: []string{"title"}, Language: "en", SortBy: "publishedAt", PageSize: 3,
		})
		require.NoError(t, err)
		requireNoAPIKey(t, searched)
		require.Equal(t, SearchArticlesBranchSearched, searched.Branch, "failure: %+v", searched.Failure)
		require.LessOrEqual(t, len(searched.Value.Articles), 3)
		require.GreaterOrEqual(t, searched.Value.TotalResults, len(searched.Value.Articles))
		for _, article := range searched.Value.Articles {
			require.NotEmpty(t, article.URL)
			require.False(t, article.PublishedAt.IsZero())
		}
		t.Logf("branch=%s totalResults=%d articles=%d hasMore=%t", searched.Branch, searched.Value.TotalResults, len(searched.Value.Articles), searched.Value.HasMore)
	})
}
