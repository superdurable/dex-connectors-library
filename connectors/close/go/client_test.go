// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package close

import (
	"context"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

const testAPIKey = "test-secret-key"

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "provider-test" }
func (queryContext) StepExecutionID() string     { return "query" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func TestListLeadsRequestAndClassification(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"success", 200, `{"data":[{"id":"lead_meridian1","name":"Meridian Labs"}],"has_more":false}`, ListLeadsBranchListed, false},
		{"unauthorized", 401, `{"error":"SENTINEL"}`, ListLeadsBranchProviderRejected, false},
		{"rate limit", 429, `{"error":"SENTINEL"}`, "", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "/lead/", request.URL.Path)
				username, password, ok := request.BasicAuth()
				require.True(t, ok)
				require.Equal(t, testAPIKey, username)
				require.Empty(t, password)
				response.WriteHeader(testCase.status)
				_, err := response.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "close", Name: "close"}
			client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.ListLeads(), reference, ListLeadsInput{})
			if testCase.retry {
				require.Error(t, err)
				require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
				require.NotContains(t, err.Error(), "SENTINEL")
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			if testCase.status == 200 {
				require.Len(t, result.Value.Leads, 1)
			} else {
				require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
		})
	}
}

func TestGetLeadRequestAndClassification(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"success", 200, `{"id":"lead_meridian1","name":"Meridian Labs"}`, GetLeadBranchFound, false},
		{"unauthorized", 401, `{"error":"SENTINEL"}`, GetLeadBranchProviderRejected, false},
		{"not found", 404, `{"error":"SENTINEL"}`, GetLeadBranchNotFound, false},
		{"rate limit", 429, `{"error":"SENTINEL"}`, "", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "/lead/lead_meridian1/", request.URL.Path)
				username, password, ok := request.BasicAuth()
				require.True(t, ok)
				require.Equal(t, testAPIKey, username)
				require.Empty(t, password)
				response.WriteHeader(testCase.status)
				_, err := response.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "close", Name: "close"}
			client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.GetLead(), reference, GetLeadInput{LeadID: "lead_meridian1"})
			if testCase.retry {
				require.Error(t, err)
				require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
				require.NotContains(t, err.Error(), "SENTINEL")
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			if testCase.status == 200 {
				require.Equal(t, "Meridian Labs", result.Value.Name)
			} else {
				if testCase.status == 404 {
					require.Equal(t, sdkgo.FailureNotFound, result.Failure.Kind)
				} else {
					require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
				}
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
		})
	}
}
