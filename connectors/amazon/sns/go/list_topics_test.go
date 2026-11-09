// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
package sns

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "amazon-sns", Name: "amazon-sns"}

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "sns-provider-test" }
func (queryContext) StepExecutionID() string     { return "list-topics" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }
func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{AccessKeyID: sdkgo.NewSecretString("AKIASNSCONNECTOR"), SecretAccessKey: sdkgo.NewSecretString("sns-test-secret"), SessionToken: sdkgo.NewSecretString("")}
	client, err := New(Config{Region: "eu-west-1", Endpoint: endpoint, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, signingTimeOption{now: func() time.Time { return time.Date(2026, 10, 30, 0, 0, 0, 0, time.UTC) }})
	require.NoError(t, err)
	return client
}

const topicResponse = `<ListTopicsResponse xmlns="https://sns.amazonaws.com/doc/2010-03-31/"><ListTopicsResult><Topics><member><TopicArn>arn:aws:sns:eu-west-1:123456789012:alerts</TopicArn></member></Topics><NextToken>next-page</NextToken></ListTopicsResult></ListTopicsResponse>`

func TestListTopicsSignsAndReadsPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodGet, r.Method)
		require.Equal(t, "/", r.URL.Path)
		require.Equal(t, "ListTopics", r.URL.Query().Get("Action"))
		require.Equal(t, "2010-03-31", r.URL.Query().Get("Version"))
		require.Equal(t, "page-token", r.URL.Query().Get("NextToken"))
		require.Equal(t, "20261030T000000Z", r.Header.Get("X-Amz-Date"))
		require.True(t, strings.Contains(r.Header.Get("Authorization"), "/eu-west-1/sns/aws4_request"))
		_, err := w.Write([]byte(topicResponse))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListTopics(), testConnection, ListTopicsInput{NextToken: "page-token"})
	require.NoError(t, err)
	require.Equal(t, ListTopicsBranchPage, result.Branch)
	require.Equal(t, TopicPage{TopicARNs: []string{"arn:aws:sns:eu-west-1:123456789012:alerts"}, NextToken: "next-page"}, result.Value)
}
func TestListTopicsClassifiesOutcomes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{"forbidden", 403, `<ErrorResponse/>`, ListTopicsBranchProviderRejected, false},
		{"rate limit", 429, `<ErrorResponse/>`, "", true},
		{"unavailable", 503, `<ErrorResponse/>`, "", true},
		{"bad response", 200, `<not-sns/>`, ListTopicsBranchInvalidResponse, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, err := w.Write([]byte(tc.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListTopics(), testConnection, ListTopicsInput{})
			if tc.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.branch, result.Branch)
		})
	}
}
func TestListTopicsRejectsInvalidTokenAndBoundsResponse(t *testing.T) {
	client := newTestClient(t, "https://sns.eu-west-1.amazonaws.com", 0)
	result, err := sdkgo.RunQuery(queryContext{}, client.ListTopics(), testConnection, ListTopicsInput{NextToken: "bad\nvalue"})
	require.NoError(t, err)
	require.Equal(t, ListTopicsBranchDefect, result.Branch)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, err := w.Write([]byte(topicResponse))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err = sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 10).ListTopics(), testConnection, ListTopicsInput{})
	require.NoError(t, err)
	require.Equal(t, ListTopicsBranchInvalidResponse, result.Branch)
}
