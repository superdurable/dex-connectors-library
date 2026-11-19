// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sentry

import (
	"context"
	"errors"
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

type providerTestContext struct{ dex.Context }

func (providerTestContext) FlowID() string              { return "provider-test" }
func (providerTestContext) StepExecutionID() string     { return "provider-step" }
func (providerTestContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (providerTestContext) Done() <-chan struct{}       { return nil }
func (providerTestContext) Err() error                  { return nil }
func (providerTestContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "sentry", Name: "sentry"}

func TestListIssuesHTTPAndFailure(t *testing.T) {
	reject := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "GET", request.Method)
		require.Equal(t, "/organizations/acme/issues/", request.URL.RequestURI())
		require.Equal(t, "Bearer test-secret-token", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		require.NotContains(t, request.URL.String(), "test-secret-token")

		if reject {
			writer.WriteHeader(400)
			return
		}
		_, err := io.WriteString(writer, `[{"id":"123","shortId":"APP-1","title":"ExampleError","status":"unresolved"}]`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(providerTestContext{}, client.ListIssues(), testConnection, ListIssuesInput{Organization: "acme"})
	require.NoError(t, err)
	require.Equal(t, ListIssuesBranchListed, result.Branch)
	require.Nil(t, result.Failure)
	reject = true
	rejected, err := sdkgo.RunQuery(providerTestContext{}, client.ListIssues(), testConnection, ListIssuesInput{Organization: "acme"})
	require.NoError(t, err)
	require.Equal(t, ListIssuesBranchProviderRejected, rejected.Branch)
	require.NotNil(t, rejected.Failure)
}

func TestGetIssueHTTPAndFailure(t *testing.T) {
	reject := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "GET", request.Method)
		require.Equal(t, "/organizations/acme/issues/123/", request.URL.RequestURI())
		require.Equal(t, "Bearer test-secret-token", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		require.NotContains(t, request.URL.String(), "test-secret-token")

		if reject {
			writer.WriteHeader(404)
			return
		}
		_, err := io.WriteString(writer, `{"id":"123","shortId":"APP-1","title":"ExampleError","status":"unresolved"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(providerTestContext{}, client.GetIssue(), testConnection, GetIssueInput{Organization: "acme", ID: "123"})
	require.NoError(t, err)
	require.Equal(t, GetIssueBranchFound, result.Branch)
	require.Nil(t, result.Failure)
	reject = true
	rejected, err := sdkgo.RunQuery(providerTestContext{}, client.GetIssue(), testConnection, GetIssueInput{Organization: "acme", ID: "123"})
	require.NoError(t, err)
	require.Equal(t, GetIssueBranchNotFound, rejected.Branch)
	require.NotNil(t, rejected.Failure)
}

func TestUpdateIssueStatusHTTPAndFailure(t *testing.T) {
	reject := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "PUT", request.Method)
		require.Equal(t, "/organizations/acme/issues/123/", request.URL.RequestURI())
		require.Equal(t, "Bearer test-secret-token", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		require.NotContains(t, request.URL.String(), "test-secret-token")
		requestBody, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"status":"resolved"}`, string(requestBody))

		if reject {
			writer.WriteHeader(404)
			_, err = io.WriteString(writer, `{"detail":"Issue not found"}`)
			require.NoError(t, err)
			return
		}
		_, err = io.WriteString(writer, `{"id":"123","shortId":"APP-1","title":"ExampleError","status":"resolved"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")}})
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(providerTestContext{}, client.UpdateIssueStatus(), testConnection, UpdateIssueStatusInput{Organization: "acme", ID: "123", Status: "resolved"})
	require.NoError(t, err)
	require.Equal(t, UpdateIssueStatusBranchUpdated, result.Branch)
	require.Nil(t, result.Failure)
	reject = true
	rejected, err := sdkgo.RunMutation(providerTestContext{}, client.UpdateIssueStatus(), testConnection, UpdateIssueStatusInput{Organization: "acme", ID: "123", Status: "resolved"})
	require.NoError(t, err)
	require.Equal(t, UpdateIssueStatusBranchNotFound, rejected.Branch)
	require.NotNil(t, rejected.Failure)
}

func TestUpdateIssueStatusRetriesUnknownAnswers(t *testing.T) {
	for name, testCase := range map[string]struct {
		status     int
		body       string
		isRejected bool
	}{
		"conflict name": {status: http.StatusConflict, body: `{"name":"invalid_idempotent_request"}`},
		"unlisted":      {status: http.StatusTooEarly, body: `{}`},
		"redirect":      {status: http.StatusFound},
		"malformed":     {status: http.StatusOK, body: `[]`},
		"definite 422":  {status: http.StatusUnprocessableEntity, body: `{"detail":"Invalid status"}`, isRejected: true},
		"definite 400":  {status: http.StatusBadRequest, body: `{"detail":"Invalid status"}`, isRejected: true},
	} {
		server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			writer.WriteHeader(testCase.status)
			_, _ = io.WriteString(writer, testCase.body)
		}))
		client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")}})
		require.NoError(t, err)
		result, err := sdkgo.RunMutation(providerTestContext{}, client.UpdateIssueStatus(), testConnection, UpdateIssueStatusInput{Organization: "acme", ID: "123", Status: "resolved"})
		server.Close()
		if testCase.isRejected {
			require.NoError(t, err, name)
			require.Equal(t, UpdateIssueStatusBranchProviderRejected, result.Branch, name)
			continue
		}
		var retry *sdkgo.RetryError
		require.ErrorAs(t, err, &retry, name)
	}
}

// failingCredentials fails every resolution with one error.
type failingCredentials struct{ err error }

func (credentials failingCredentials) Resolve(sdkgo.Call) (Credentials, error) {
	return Credentials{}, credentials.err
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
			result, err := sdkgo.RunMutation(providerTestContext{}, client.UpdateIssueStatus(), testConnection, UpdateIssueStatusInput{Organization: "acme", ID: "123", Status: "resolved"})
			if test.isTerminal {
				require.NoError(t, err)
				require.Equal(t, UpdateIssueStatusBranchDefect, result.Branch)
				return
			}
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.Equal(t, sdkgo.FailureAvailability, retry.Failure.Kind)
		})
	}
}
