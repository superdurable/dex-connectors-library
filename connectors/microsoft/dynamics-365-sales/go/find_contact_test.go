// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package dynamics365sales

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

var testConnection = sdkgo.ConnectionRef{Provider: "microsoft-dynamics-365-sales", Name: "microsoft-dynamics-365-sales"}

const testTenantID = "11111111-1111-4111-8111-111111111111"
const testClientID = "22222222-2222-4222-8222-222222222222"
const testContactID = "33333333-3333-4333-8333-333333333333"

type queryContext struct{ dex.Context }

func (queryContext) FlowID() string              { return "dynamics-contact-provider-test" }
func (queryContext) StepExecutionID() string     { return "find-contact" }
func (queryContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (queryContext) Done() <-chan struct{}       { return nil }
func (queryContext) Err() error                  { return nil }
func (queryContext) Value(key any) any           { return context.Background().Value(key) }

func newTestClient(t *testing.T, endpoint string, maxResponseBytes int64) *Client {
	t.Helper()
	credentials := Credentials{ClientSecret: sdkgo.NewSecretString("test-client-secret")}
	client, err := New(Config{EnvironmentURL: endpoint, TenantID: testTenantID, ClientID: testClientID, MaxResponseBytes: maxResponseBytes}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: credentials}, WithTokenEndpoint(endpoint+"/token"))
	require.NoError(t, err)
	return client
}

func TestFindContactGetsTokenAndSearchesEmail(t *testing.T) {
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		switch r.URL.Path {
		case "/token":
			require.Equal(t, http.MethodPost, r.Method)
			require.NoError(t, r.ParseForm())
			require.Equal(t, testClientID, r.PostForm.Get("client_id"))
			require.Equal(t, "test-client-secret", r.PostForm.Get("client_secret"))
			require.Equal(t, "client_credentials", r.PostForm.Get("grant_type"))
			require.Equal(t, "http://"+r.Host+"/.default", r.PostForm.Get("scope"))
			_, err := w.Write([]byte(`{"access_token":"test-access-token","token_type":"Bearer"}`))
			require.NoError(t, err)
		case "/api/data/v9.2/contacts":
			require.Equal(t, "Bearer test-access-token", r.Header.Get("Authorization"))
			require.Equal(t, "emailaddress1 eq 'ada@example.com'", r.URL.Query().Get("$filter"))
			require.Equal(t, "contactid,fullname,emailaddress1", r.URL.Query().Get("$select"))
			require.Equal(t, "2", r.URL.Query().Get("$top"))
			_, err := w.Write([]byte(`{"value":[{"contactid":"` + testContactID + `","fullname":"Ada Example","emailaddress1":"ada@example.com"}]}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %q", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).FindContact(), testConnection, FindContactInput{EmailAddress: "ada@example.com"})
	require.NoError(t, err)
	require.Equal(t, FindContactBranchFound, result.Branch)
	require.Equal(t, Contact{ContactID: testContactID, FullName: "Ada Example", EmailAddress: "ada@example.com"}, result.Value)
	require.Equal(t, 2, requests)
}

func TestFindContactClassifiesProviderOutcomes(t *testing.T) {
	for _, scenario := range []struct {
		name          string
		tokenStatus   int
		contactStatus int
		contactBody   string
		branch        sdkgo.BranchID
		retry         bool
	}{
		{name: "missing", contactBody: `{"value":[]}`, branch: FindContactBranchMissing},
		{name: "multiple", contactBody: `{"value":[{"contactid":"` + testContactID + `"},{"contactid":"` + testContactID + `"}]}`, branch: FindContactBranchMultipleMatches},
		{name: "malformed", contactBody: `{"value":null}`, branch: FindContactBranchInvalidResponse},
		{name: "token rejected", tokenStatus: 401, branch: FindContactBranchProviderRejected},
		{name: "token rate limited", tokenStatus: 429, retry: true},
		{name: "contact rejected", contactStatus: 403, branch: FindContactBranchProviderRejected},
		{name: "contact unavailable", contactStatus: 503, retry: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == "/token" {
					if scenario.tokenStatus != 0 {
						w.WriteHeader(scenario.tokenStatus)
						return
					}
					_, err := w.Write([]byte(`{"access_token":"test-access-token","token_type":"Bearer"}`))
					require.NoError(t, err)
					return
				}
				if scenario.contactStatus != 0 {
					w.WriteHeader(scenario.contactStatus)
					return
				}
				_, err := w.Write([]byte(scenario.contactBody))
				require.NoError(t, err)
			}))
			defer server.Close()
			result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).FindContact(), testConnection, FindContactInput{EmailAddress: "ada@example.com"})
			if scenario.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, scenario.branch, result.Branch)
		})
	}
}

func TestFindContactRejectsInvalidInput(t *testing.T) {
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, "http://127.0.0.1:1", 0).FindContact(), testConnection, FindContactInput{EmailAddress: "bad\r\nvalue"})
	require.NoError(t, err)
	require.Equal(t, FindContactBranchDefect, result.Branch)
}
