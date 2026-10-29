// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package figma

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

func TestGetFileRequestAndClassification(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"success", 200, `{"name":"Meridian Design","lastModified":"2026-10-28T12:00:00Z","document":{"id":"0:0","type":"DOCUMENT"}}`, GetFileBranchFound, false},
		{"unauthorized", 401, `{"error":"SENTINEL"}`, GetFileBranchProviderRejected, false},
		{"not found", 404, `{"error":"SENTINEL"}`, GetFileBranchNotFound, false},
		{"rate limit", 429, `{"error":"SENTINEL"}`, "", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "/files/fileA", request.URL.Path)
				require.Equal(t, testAPIKey, request.Header.Get("X-Figma-Token"))
				response.WriteHeader(testCase.status)
				_, err := response.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "figma", Name: "figma"}
			client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.GetFile(), reference, GetFileInput{FileKey: "fileA"})
			if testCase.retry {
				require.Error(t, err)
				require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
				require.NotContains(t, err.Error(), "SENTINEL")
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			if testCase.status == 200 {
				require.Equal(t, "Meridian Design", result.Value.Name)
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

func TestGetFileCommentsRequestAndClassification(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"success", 200, `{"comments":[{"id":"1","message":"Review the header."}]}`, GetFileCommentsBranchListed, false},
		{"unauthorized", 401, `{"error":"SENTINEL"}`, GetFileCommentsBranchProviderRejected, false},
		{"not found", 404, `{"error":"SENTINEL"}`, GetFileCommentsBranchNotFound, false},
		{"rate limit", 429, `{"error":"SENTINEL"}`, "", true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
				require.Equal(t, http.MethodGet, request.Method)
				require.Equal(t, "/files/fileA/comments", request.URL.Path)
				require.Equal(t, testAPIKey, request.Header.Get("X-Figma-Token"))
				response.WriteHeader(testCase.status)
				_, err := response.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "figma", Name: "figma"}
			client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{reference: {APIKey: sdkgo.NewSecretString(testAPIKey)}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(queryContext{}, client.GetFileComments(), reference, GetFileCommentsInput{FileKey: "fileA"})
			if testCase.retry {
				require.Error(t, err)
				require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
				require.NotContains(t, err.Error(), "SENTINEL")
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
			if testCase.status == 200 {
				require.Len(t, result.Value.Comments, 1)
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
