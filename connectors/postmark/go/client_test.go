// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postmark

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

var testConnection = sdkgo.ConnectionRef{Provider: "postmark", Name: "postmark"}

func TestListOutboundMessagesHTTPAndFailure(t *testing.T) {
	reject := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "GET", request.Method)
		require.Equal(t, "/messages/outbound?count=25&offset=0", request.URL.RequestURI())
		require.Equal(t, "test-secret-token", request.Header.Get("X-Postmark-Server-Token"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		require.NotContains(t, request.URL.String(), "test-secret-token")

		if reject {
			writer.WriteHeader(400)
			return
		}
		_, err := io.WriteString(writer, `{"TotalCount":1,"Messages":[{"MessageID":"msg-1","Subject":"Welcome","Status":"Sent"}]}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(providerTestContext{}, client.ListOutboundMessages(), testConnection, ListOutboundMessagesInput{Count: 25, Offset: 0})
	require.NoError(t, err)
	require.Equal(t, ListOutboundMessagesBranchListed, result.Branch)
	require.Nil(t, result.Failure)
	reject = true
	rejected, err := sdkgo.RunQuery(providerTestContext{}, client.ListOutboundMessages(), testConnection, ListOutboundMessagesInput{Count: 25, Offset: 0})
	require.NoError(t, err)
	require.Equal(t, ListOutboundMessagesBranchProviderRejected, rejected.Branch)
	require.NotNil(t, rejected.Failure)
}

func TestGetMessageDetailsHTTPAndFailure(t *testing.T) {
	reject := false
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		require.Equal(t, "GET", request.Method)
		require.Equal(t, "/messages/outbound/msg-1/details", request.URL.RequestURI())
		require.Equal(t, "test-secret-token", request.Header.Get("X-Postmark-Server-Token"))
		require.Equal(t, "application/json", request.Header.Get("Accept"))
		require.NotContains(t, request.URL.String(), "test-secret-token")

		if reject {
			writer.Header().Set("X-PM-ApiErrorCode", "701")
			writer.WriteHeader(422)
			return
		}
		_, err := io.WriteString(writer, `{"MessageID":"msg-1","Subject":"Welcome","Status":"Sent"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	client, err := New(Config{BaseURL: server.URL}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {APIKey: sdkgo.NewSecretString("test-secret-token")}})
	require.NoError(t, err)
	result, err := sdkgo.RunQuery(providerTestContext{}, client.GetMessageDetails(), testConnection, GetMessageDetailsInput{MessageID: "msg-1"})
	require.NoError(t, err)
	require.Equal(t, GetMessageDetailsBranchFound, result.Branch)
	require.Nil(t, result.Failure)
	reject = true
	rejected, err := sdkgo.RunQuery(providerTestContext{}, client.GetMessageDetails(), testConnection, GetMessageDetailsInput{MessageID: "msg-1"})
	require.NoError(t, err)
	require.Equal(t, GetMessageDetailsBranchNotFound, rejected.Branch)
	require.NotNil(t, rejected.Failure)
}
