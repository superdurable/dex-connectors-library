// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package resend

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type providerTestContext struct{ dex.Context }

func (providerTestContext) FlowID() string              { return "provider-test" }
func (providerTestContext) StepExecutionID() string     { return "provider-step" }
func (providerTestContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (providerTestContext) Done() <-chan struct{}       { return nil }
func (providerTestContext) Err() error                  { return nil }
func (providerTestContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "resend", Name: "resend"}

func TestSendEmailHTTPAndFailure(t *testing.T) {
	reject := false
	var firstIdempotencyKey string
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "POST", request.Method)
		require.Equal(t, "/emails", request.URL.RequestURI())
		require.Equal(t, "Bearer test-secret-token", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		require.NotContains(t, request.URL.String(), "test-secret-token")
		idempotencyKey := request.Header.Get("Idempotency-Key")
		require.NotEmpty(t, idempotencyKey)
		if firstIdempotencyKey != "" {
			require.Equal(t, firstIdempotencyKey, idempotencyKey)
		}
		firstIdempotencyKey = idempotencyKey
		requestBody, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"from":"Acme <hello@example.test>","to":["reader@example.test"],"subject":"Welcome","html":"<p>Hello</p>"}`, string(requestBody))
		if reject {
			writer.WriteHeader(400)
			return
		}
		_, err = io.WriteString(writer, `{"id":"email-1"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")}})
	require.NoError(t, err)
	result, err := sdkgo.RunMutation(providerTestContext{}, client.SendEmail(), testConnection, SendEmailInput{From: "Acme <hello@example.test>", To: []string{"reader@example.test"}, Subject: "Welcome", HTML: "<p>Hello</p>"})
	require.NoError(t, err)
	require.Equal(t, SendEmailBranchSent, result.Branch)
	require.Nil(t, result.Failure)
	reject = true
	rejected, err := sdkgo.RunMutation(providerTestContext{}, client.SendEmail(), testConnection, SendEmailInput{From: "Acme <hello@example.test>", To: []string{"reader@example.test"}, Subject: "Welcome", HTML: "<p>Hello</p>"})
	require.NoError(t, err)
	require.Equal(t, SendEmailBranchProviderRejected, rejected.Branch)
	require.NotNil(t, rejected.Failure)
}

func TestGetEmailHTTPAndFailure(t *testing.T) {
	reject := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "GET", request.Method)
		require.Equal(t, "/emails/email-1", request.URL.RequestURI())
		require.Equal(t, "Bearer test-secret-token", request.Header.Get("Authorization"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		require.NotContains(t, request.URL.String(), "test-secret-token")

		if reject {
			writer.WriteHeader(404)
			return
		}
		_, err := io.WriteString(writer, `{"id":"email-1","from":"Acme <hello@example.test>","to":["reader@example.test"],"subject":"Welcome","created_at":"2026-10-26T12:00:00Z"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(providerTestContext{}, client.GetEmail(), testConnection, GetEmailInput{ID: "email-1"})
	require.NoError(t, err)
	require.Equal(t, GetEmailBranchFound, result.Branch)
	require.Nil(t, result.Failure)
	reject = true
	rejected, err := sdkgo.RunQuery(providerTestContext{}, client.GetEmail(), testConnection, GetEmailInput{ID: "email-1"})
	require.NoError(t, err)
	require.Equal(t, GetEmailBranchNotFound, rejected.Branch)
	require.NotNil(t, rejected.Failure)
}
