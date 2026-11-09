// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sell

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

const testAccessToken = "synthetic-sell-token"

var testConnection = sdkgo.ConnectionRef{Provider: "zendesk-sell", Name: "zendesk-sell"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "zendesk-sell-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func TestListLeadsBearerAuthorizationAndPrivacy(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v2/leads", r.URL.Path)
		require.Equal(t, "page=1&per_page=2", r.URL.RawQuery)
		require.Equal(t, "Bearer "+testAccessToken, r.Header.Get("Authorization"))
		_, err := w.Write([]byte(`{"items":[{"data":{"id":144,"first_name":"Ada","last_name":"Lovelace","email":"ada@example.com","organization_name":"Example Co","custom_fields":{"private":"not-persisted"}}}],"meta":{"count":1}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	reference := testConnection
	client, err := New(Config{BaseURL: server.URL + "/v2"}, sdkgo.StaticCredentialProvider[Credentials]{reference: {AccessToken: sdkgo.NewSecretString(testAccessToken)}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListLeads(), reference, ListLeadsInput{Page: 1, PerPage: 2})
	require.NoError(t, err)
	require.Equal(t, ListLeadsBranchListed, result.Branch)
	require.Equal(t, []LeadMetadata{{ID: 144, FirstName: "Ada", LastName: "Lovelace", EmailAddress: "ada@example.com", OrganizationName: "Example Co"}}, result.Value.Leads)
}
