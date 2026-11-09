// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package facebookleadads

import (
	"context"
	"encoding/json"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

const testPageToken = "synthetic-page-token"

var testConnection = sdkgo.ConnectionRef{Provider: "facebook-lead-ads", Name: "facebook-lead-ads"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "facebook-lead-ads-provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: baseURL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PageAccessToken: sdkgo.NewSecretString(testPageToken)}})
	require.NoError(t, err)
	return client
}

func TestListLeadFormsProjectsIdentityAndCursor(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/123456789/leadgen_forms", r.URL.Path)
		require.Equal(t, "id,name,status", r.URL.Query().Get("fields"))
		require.Equal(t, "100", r.URL.Query().Get("limit"))
		require.Equal(t, "cursor/+value", r.URL.Query().Get("after"))
		require.Equal(t, "Bearer "+testPageToken, r.Header.Get("Authorization"))
		require.Empty(t, r.URL.Query().Get("access_token"))
		_, err := w.Write([]byte(`{"data":[{"id":"123456789012345","name":"Contact request","status":"ACTIVE","questions":["private"]}],"paging":{"cursors":{"after":"next-cursor"}}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListLeadForms(), testConnection, ListLeadFormsInput{PageID: "123456789", After: "cursor/+value"})
	require.NoError(t, err)
	require.Equal(t, ListLeadFormsBranchListed, result.Branch)
	require.Equal(t, "next-cursor", result.Value.NextCursor)
	require.Equal(t, "Contact request", result.Value.Forms[0].Name)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
	require.NotContains(t, string(encoded), testPageToken)
}

func TestGetLeadProjectsOnlyMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/987654321012345", r.URL.Path)
		require.Equal(t, "id,created_time,form_id,ad_id", r.URL.Query().Get("fields"))
		_, err := w.Write([]byte(`{"id":"987654321012345","form_id":"123456789012345","ad_id":"112233445566778","created_time":"2026-11-08T10:00:00+0000","field_data":[{"name":"email","values":["private@example.com"]}]}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	result, err := sdkgo.RunQuery(queryContext{}, client.GetLead(), testConnection, GetLeadInput{LeadID: "987654321012345"})
	require.NoError(t, err)
	require.Equal(t, GetLeadBranchFound, result.Branch)
	require.Equal(t, "123456789012345", result.Value.FormID)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private@example.com")
}

func TestGraphFailuresAndLocalValidation(t *testing.T) {
	var calls atomic.Int32
	var status atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(int(status.Load()))
		_, err := w.Write([]byte(`{"error":{"message":"unavailable"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	invalid, err := sdkgo.RunQuery(queryContext{}, client.ListLeadForms(), testConnection, ListLeadFormsInput{PageID: "bad/id"})
	require.NoError(t, err)
	require.Equal(t, ListLeadFormsBranchDefect, invalid.Branch)
	require.Zero(t, calls.Load())
	for _, code := range []int{http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		status.Store(int32(code))
		_, err := sdkgo.RunQuery(queryContext{}, client.GetLead(), testConnection, GetLeadInput{LeadID: "987654321012345"})
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry)
		require.NotContains(t, err.Error(), testPageToken)
	}
	status.Store(http.StatusNotFound)
	missing, err := sdkgo.RunQuery(queryContext{}, client.GetLead(), testConnection, GetLeadInput{LeadID: "987654321012345"})
	require.NoError(t, err)
	require.Equal(t, GetLeadBranchNotFound, missing.Branch)
	status.Store(http.StatusForbidden)
	rejected, err := sdkgo.RunQuery(queryContext{}, client.ListLeadForms(), testConnection, ListLeadFormsInput{PageID: "123456789"})
	require.NoError(t, err)
	require.Equal(t, ListLeadFormsBranchProviderRejected, rejected.Branch)
	require.NotContains(t, rejected.Failure.Message, testPageToken)
}

func TestGraphRejectsMalformedProviderResults(t *testing.T) {
	var body atomic.Value
	body.Store(`{"data":{}}`)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(body.Load().(string)))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL)
	forms, err := sdkgo.RunQuery(queryContext{}, client.ListLeadForms(), testConnection, ListLeadFormsInput{PageID: "123456789"})
	require.NoError(t, err)
	require.Equal(t, ListLeadFormsBranchInvalidResponse, forms.Branch)
	body.Store(`{"id":"987654321012345","form_id":"bad/id","created_time":"2026-11-08T10:00:00+0000"}`)
	lead, err := sdkgo.RunQuery(queryContext{}, client.GetLead(), testConnection, GetLeadInput{LeadID: "987654321012345"})
	require.NoError(t, err)
	require.Equal(t, GetLeadBranchInvalidResponse, lead.Branch)
}
