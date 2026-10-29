// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package contentful

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type testContext struct{ dex.Context }

func (testContext) FlowID() string              { return "provider-test" }
func (testContext) StepExecutionID() string     { return "provider-step" }
func (testContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (testContext) Done() <-chan struct{}       { return nil }
func (testContext) Err() error                  { return nil }
func (testContext) Value(key any) any           { return context.Background().Value(key) }

func TestGetEntryHTTPAndRejection(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"happy", 200, `{"sys":{"id":"entry-meridian","type":"Entry"},"fields":{"title":"Meridian"}}`, GetEntryBranchFound},
		{"rejected", 401, `{"message":"SECRET SENTINEL"}`, GetEntryBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/spaces/space123/environments/master/entries/entry-meridian", r.URL.Path)
				require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				require.NotContains(t, r.URL.String(), "test-token")
				w.WriteHeader(test.status)
				_, err := fmt.Fprint(w, test.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "contentful", Name: "contentful"}
			client, err := New(Config{BaseURL: server.URL + ""}, sdkgo.StaticCredentialProvider[Credentials]{reference: {DeliveryToken: sdkgo.NewSecretString("test-token")}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(testContext{}, client.GetEntry(), reference, GetEntryInput{SpaceID: "space123", EnvironmentID: "master", EntryID: "entry-meridian"})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.name == "rejected" {
				require.NotNil(t, result.Failure)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
		})
	}
}

func TestListEntriesHTTPAndRejection(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"happy", 200, `{"items":[],"total":0,"skip":0,"limit":10}`, ListEntriesBranchListed},
		{"rejected", 401, `{"message":"SECRET SENTINEL"}`, ListEntriesBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/spaces/space123/environments/master/entries", r.URL.Path)
				require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				require.NotContains(t, r.URL.String(), "test-token")
				w.WriteHeader(test.status)
				_, err := fmt.Fprint(w, test.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "contentful", Name: "contentful"}
			client, err := New(Config{BaseURL: server.URL + ""}, sdkgo.StaticCredentialProvider[Credentials]{reference: {DeliveryToken: sdkgo.NewSecretString("test-token")}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(testContext{}, client.ListEntries(), reference, ListEntriesInput{SpaceID: "space123", EnvironmentID: "master", Limit: 10})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.name == "rejected" {
				require.NotNil(t, result.Failure)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
		})
	}
}
