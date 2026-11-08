// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package odoo

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

var testConnection = sdkgo.ConnectionRef{Provider: "odoo", Name: "odoo"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "odoo-provider-test" }
func (queryContext) StepExecutionID() string     { return "search-partners" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{APIKey: sdkgo.NewSecretString("sample-odoo-api-key")}
	client, err := New(Config{BaseURL: endpoint, Database: "mycompany", MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials})
	require.NoError(t, err)
	return client
}

func TestSearchPartnersUsesOneBoundedJSON2Call(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/json/2/res.partner/search_read", request.URL.Path)
		require.Equal(t, "Bearer sample-odoo-api-key", request.Header.Get("Authorization"))
		require.Equal(t, "mycompany", request.Header.Get("X-Odoo-Database"))
		var body struct {
			Domain [][]any  `json:"domain"`
			Fields []string `json:"fields"`
			Limit  int      `json:"limit"`
		}
		require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
		require.Equal(t, [][]any{{"is_company", "=", true}, {"name", "ilike", "%Acme%"}}, body.Domain)
		require.Equal(t, []string{"id", "name"}, body.Fields)
		require.Equal(t, 5, body.Limit)
		_, err := writer.Write([]byte(`[{"id":42,"name":"Acme Company","email":"secret@example.com"}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).SearchPartners(), testConnection, SearchPartnersInput{NamePattern: "%Acme%", Limit: 5})
	require.NoError(t, err)
	require.Equal(t, SearchPartnersBranchFound, result.Branch)
	require.Equal(t, PartnerSearchPage{Partners: []Partner{{ID: 42, Name: "Acme Company"}}}, result.Value)
}

func TestSearchPartnersClassifiesProviderOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "empty", status: 200, body: `[]`, branch: SearchPartnersBranchFound},
		{name: "forbidden", status: 403, branch: SearchPartnersBranchProviderRejected},
		{name: "rate limit", status: 429, retry: true},
		{name: "unavailable", status: 503, retry: true},
		{name: "invalid partner", status: 200, body: `[{"id":0,"name":"Bad"}]`, branch: SearchPartnersBranchInvalidResponse},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).SearchPartners(), testConnection, SearchPartnersInput{NamePattern: "%Acme%", Limit: 5})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			if testCase.name == "empty" {
				require.Equal(t, []Partner{}, result.Value.Partners)
			}
		})
	}
}

func TestSearchPartnersRejectsInvalidInputAndOversizedResponse(t *testing.T) {
	client := newTestClient(t, "https://odoo.example.com", 0)
	for _, input := range []SearchPartnersInput{{NamePattern: ""}, {NamePattern: " bad "}, {NamePattern: "%Acme%", Limit: 101}} {
		result, err := sdkgo.RunQuery(queryContext{}, client.SearchPartners(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, SearchPartnersBranchDefect, result.Branch)
	}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, err := writer.Write([]byte(`[{"id":42,"name":"Acme Company"}]`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 8).SearchPartners(), testConnection, SearchPartnersInput{NamePattern: "%Acme%", Limit: 5})
	require.NoError(t, err)
	require.Equal(t, SearchPartnersBranchInvalidResponse, result.Branch)
}
