// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package constantcontact

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type expiringCreationCredentials struct{ resolves int }

func (provider *expiringCreationCredentials) Resolve(sdkgo.Call) (Credentials, error) {
	provider.resolves++
	if provider.resolves > 2 {
		return Credentials{}, nil
	}
	return testCredentials(), nil
}

func (provider *expiringCreationCredentials) ResolveWithRefresh(_ context.Context, call sdkgo.Call, _ sdkgo.CredentialRefreshDriver[Credentials]) (Credentials, error) {
	return provider.Resolve(call)
}

func TestCreateRetriesWhenCredentialsExpireAfterPossibleSend(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		readback string
		run      func(*listConformanceContext, *Client) error
	}{
		{
			name: "contact", readback: `{"contacts":[]}`,
			run: func(ctx *listConformanceContext, client *Client) error {
				_, err := sdkgo.RunMutation(ctx, client.CreateContact(), testConnection, CreateContactInput{
					EmailAddress: "new-reader@example.com", EmailPermission: "implicit", CreateSource: "Account",
				})
				return err
			},
		},
		{
			name: "contact list", readback: `{"lists":[]}`,
			run: func(ctx *listConformanceContext, client *Client) error {
				_, err := sdkgo.RunMutation(ctx, client.CreateContactList(), testConnection, CreateContactListInput{Name: "Customers"})
				return err
			},
		},
		{
			name: "segment", readback: `{"segments":[]}`,
			run: func(ctx *listConformanceContext, client *Client) error {
				_, err := sdkgo.RunMutation(ctx, client.CreateSegment(), testConnection, CreateSegmentInput{
					Name: "Recent readers", SegmentCriteria: `{"version":"1.0.0","criteria":{"type":"and","group":[]}}`,
				})
				return err
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			requestCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				requestCount++
				if request.Method == http.MethodPost {
					writer.WriteHeader(http.StatusInternalServerError)
					return
				}
				require.Equal(t, http.MethodGet, request.Method)
				_, err := writer.Write([]byte(testCase.readback))
				require.NoError(t, err)
			}))
			defer server.Close()
			credentials := &expiringCreationCredentials{}
			client, err := New(Config{BaseURL: server.URL}, credentials)
			require.NoError(t, err)
			ctx := &listConformanceContext{attempt: 1}
			var retry *sdkgo.RetryError
			require.ErrorAs(t, testCase.run(ctx, client), &retry)
			ctx.attempt = 2
			require.ErrorAs(t, testCase.run(ctx, client), &retry)
			require.Equal(t, 2, requestCount)
			require.Equal(t, 3, credentials.resolves)
		})
	}
}
