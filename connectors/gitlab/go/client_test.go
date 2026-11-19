// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gitlab

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/projectconfig"
	"github.com/superdurable/dex/sdk-go/dex"
)

type testContext struct{ dex.Context }

func (testContext) FlowID() string              { return "provider-test" }
func (testContext) StepExecutionID() string     { return "test" }
func (testContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (testContext) Done() <-chan struct{}       { return nil }
func (testContext) Err() error                  { return nil }
func (testContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "gitlab", Name: "gitlab"}

func testClient(t *testing.T, endpoint string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: endpoint}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString("test-accesstoken")}})
	require.NoError(t, err)
	return client
}

func TestGetProjectProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/projects/meridian%2Fweb", r.URL.EscapedPath())
		require.Equal(t, "test-accesstoken", r.Header.Get("PRIVATE-TOKEN"))
		require.NotContains(t, r.URL.RawQuery, "test-")
		_, err := fmt.Fprint(w, `{"id":42,"name":"Meridian Web","path_with_namespace":"meridian/web","web_url":"https://gitlab.com/meridian/web"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	result, err := sdkgo.RunQuery(testContext{}, client.GetProject(), testConnection, GetProjectInput{ProjectID: "meridian/web"})
	require.NoError(t, err)
	require.Equal(t, GetProjectBranchFound, result.Branch)
	require.Nil(t, result.Failure)
}
func TestGetProjectRetriesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer server.Close()
	client := testClient(t, server.URL)
	_, err := sdkgo.RunQuery(testContext{}, client.GetProject(), testConnection, GetProjectInput{ProjectID: "meridian/web"})
	require.Error(t, err)
	require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
}

func TestListIssuesProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/projects/meridian%2Fweb/issues", r.URL.EscapedPath())
		require.Equal(t, "test-accesstoken", r.Header.Get("PRIVATE-TOKEN"))
		require.NotContains(t, r.URL.RawQuery, "test-")
		_, err := fmt.Fprint(w, `[{"id":123,"iid":7,"title":"Review release notes","labels":["review"],"web_url":"https://gitlab.com/meridian/web/-/issues/7"}]`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	result, err := sdkgo.RunQuery(testContext{}, client.ListIssues(), testConnection, ListIssuesInput{ProjectID: "meridian/web"})
	require.NoError(t, err)
	require.Equal(t, ListIssuesBranchListed, result.Branch)
	require.Nil(t, result.Failure)
}
func TestListIssuesRetriesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer server.Close()
	client := testClient(t, server.URL)
	_, err := sdkgo.RunQuery(testContext{}, client.ListIssues(), testConnection, ListIssuesInput{ProjectID: "meridian/web"})
	require.Error(t, err)
	require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
}

func TestUpdateIssueLabelsProviderResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "PUT", r.Method)
		require.Equal(t, "/projects/meridian%2Fweb/issues/7", r.URL.EscapedPath())
		require.Equal(t, "test-accesstoken", r.Header.Get("PRIVATE-TOKEN"))
		require.Equal(t, "application/x-www-form-urlencoded", r.Header.Get("Content-Type"))
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, "labels=review%2Cready", string(body))
		require.NotContains(t, r.URL.RawQuery, "test-")
		_, err = fmt.Fprint(w, `{"id":123,"iid":7,"title":"Review release notes","labels":["review","ready"],"web_url":"https://gitlab.com/meridian/web/-/issues/7"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	result, err := sdkgo.RunMutation(testContext{}, client.UpdateIssueLabels(), testConnection, UpdateIssueLabelsInput{ProjectID: "meridian/web", IssueIID: 7, Labels: []string{"review", "ready"}})
	require.NoError(t, err)
	require.Equal(t, UpdateIssueLabelsBranchUpdated, result.Branch)
	require.Nil(t, result.Failure)
}
func TestUpdateIssueLabelsRetriesRateLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) }))
	defer server.Close()
	client := testClient(t, server.URL)
	_, err := sdkgo.RunMutation(testContext{}, client.UpdateIssueLabels(), testConnection, UpdateIssueLabelsInput{ProjectID: "meridian/web", IssueIID: 7, Labels: []string{"review", "ready"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), string(sdkgo.FailureRateLimit))
}

// failingCredentials fails every resolution with one error.
type failingCredentials struct{ err error }

func (credentials failingCredentials) Resolve(sdkgo.Call) (Credentials, error) {
	return Credentials{}, credentials.err
}

func TestUpdateIssueLabelsRetriesUnknownAnswers(t *testing.T) {
	for name, testCase := range map[string]struct {
		status     int
		body       string
		isRejected bool
	}{
		"redirect":     {status: http.StatusFound},
		"unlisted":     {status: http.StatusTooEarly, body: `{}`},
		"malformed":    {status: http.StatusOK, body: `{"id":1,"iid":0}`},
		"definite 422": {status: http.StatusUnprocessableEntity, body: `{"message":"Invalid labels"}`, isRejected: true},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(testCase.status)
			_, _ = io.WriteString(writer, testCase.body)
		}))
		client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {AccessToken: sdkgo.NewSecretString("test-accesstoken")}})
		require.NoError(t, err)
		result, err := sdkgo.RunMutation(testContext{}, client.UpdateIssueLabels(), testConnection, UpdateIssueLabelsInput{ProjectID: "meridian/web", IssueIID: 7, Labels: []string{"review"}})
		server.Close()
		if testCase.isRejected {
			require.NoError(t, err, name)
			require.Equal(t, UpdateIssueLabelsBranchProviderRejected, result.Branch, name)
			continue
		}
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, name)
	}
}

func TestCredentialResolveErrorsUseTheSharedTerminalSet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("no request may be sent") }))
	defer server.Close()
	for name, test := range map[string]struct {
		err        error
		isTerminal bool
	}{
		"reauthorization":         {sdkgo.ErrReauthorizationRequired, true},
		"project reauthorization": {projectconfig.ErrReauthorizationRequired, true},
		"object not found":        {projectconfig.ErrObjectNotFound, true},
		"connection not selected": {projectconfig.ErrConnectionNotSelected, true},
		"broker rejected":         {projectconfig.ErrCredentialBrokerRejected, true},
		"broker unavailable":      {projectconfig.ErrCredentialBrokerUnavailable, false},
		"not configured":          {errors.New("connection is not configured"), false},
	} {
		t.Run(name, func(t *testing.T) {
			client, err := New(Config{BaseURL: server.URL}, failingCredentials{test.err})
			require.NoError(t, err)
			result, err := sdkgo.RunMutation(testContext{}, client.UpdateIssueLabels(), testConnection, UpdateIssueLabelsInput{ProjectID: "meridian/web", IssueIID: 7, Labels: []string{"review"}})
			if test.isTerminal {
				require.NoError(t, err)
				require.Equal(t, UpdateIssueLabelsBranchDefect, result.Branch)
				return
			}
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
		})
	}
}

func TestUpdateIssueLabelsCanClearTheEntireSet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, "labels=", string(body))
		_, err = fmt.Fprint(w, `{"id":123,"iid":7,"title":"Review release notes","labels":[]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client := testClient(t, server.URL)
	result, err := sdkgo.RunMutation(testContext{}, client.UpdateIssueLabels(), testConnection, UpdateIssueLabelsInput{ProjectID: "meridian/web", IssueIID: 7})
	require.NoError(t, err)
	require.Equal(t, UpdateIssueLabelsBranchUpdated, result.Branch)
	require.Empty(t, result.Value.Labels)
}
