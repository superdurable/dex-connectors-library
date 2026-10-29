// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package smartsheet

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

func TestListSheetsRequestAndClassification(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"success", 200, `{"pageNumber":1,"totalCount":1,"data":[{"id":1001,"name":"Meridian Planning"}]}`, ListSheetsBranchListed, false},
		{"unauthorized", 401, `{"error":"SENTINEL"}`, ListSheetsBranchProviderRejected, false},
		{"rate limit", 429, `{"error":"SENTINEL"}`, "", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "/sheets", request.URL.Path)
				require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
				response.WriteHeader(testCase.status)
				_, err := response.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "smartsheet", Name: "smartsheet"}
			client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.ListSheets(), reference, ListSheetsInput{})
			if testCase.retry {
				require.Error(t, err)
				require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
				require.NotContains(t, err.Error(), "SENTINEL")
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			if testCase.status == 200 {
				require.Len(t, result.Value.Sheets, 1)
			} else {
				require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
		})
	}
}

func TestGetSheetRequestAndClassification(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"success", 200, `{"id":1001,"name":"Meridian Planning","columns":[{"id":2001,"title":"Task"}],"rows":[]}`, GetSheetBranchFound, false},
		{"unauthorized", 401, `{"error":"SENTINEL"}`, GetSheetBranchProviderRejected, false},
		{"not found", 404, `{"error":"SENTINEL"}`, GetSheetBranchNotFound, false},
		{"rate limit", 429, `{"error":"SENTINEL"}`, "", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "/sheets/1001", request.URL.Path)
				require.Equal(t, "Bearer "+testAPIKey, request.Header.Get("Authorization"))
				response.WriteHeader(testCase.status)
				_, err := response.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "smartsheet", Name: "smartsheet"}
			client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.GetSheet(), reference, GetSheetInput{SheetID: 1001})
			if testCase.retry {
				require.Error(t, err)
				require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
				require.NotContains(t, err.Error(), "SENTINEL")
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			if testCase.status == 200 {
				require.Equal(t, int64(1001), result.Value.ID)
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
