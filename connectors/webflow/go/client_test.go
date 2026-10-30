// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package webflow

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

func TestListCollectionItemsHTTPAndRejection(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"happy", 200, `{"items":[],"pagination":{"total":0,"limit":10,"offset":0}}`, ListCollectionItemsBranchListed},
		{"rejected", 401, `{"message":"SECRET SENTINEL"}`, ListCollectionItemsBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v2/collections/62b720ef280c7a7a3be8cabe/items", r.URL.Path)
				require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				require.NotContains(t, r.URL.String(), "test-token")
				w.WriteHeader(test.status)
				_, err := fmt.Fprint(w, test.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "webflow", Name: "webflow"}
			client, err := New(Config{BaseURL: server.URL + "/v2"}, sdkgo.StaticCredentialProvider[Credentials]{reference: {SiteToken: sdkgo.NewSecretString("test-token")}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(testContext{}, client.ListCollectionItems(), reference, ListCollectionItemsInput{CollectionID: "62b720ef280c7a7a3be8cabe", Limit: 10})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.name == "rejected" {
				require.NotNil(t, result.Failure)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
		})
	}
}

func TestGetCollectionItemHTTPAndRejection(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
	}{
		{"happy", 200, `{"id":"62b720ef280c7a7a3be8cabe","fieldData":{"name":"Meridian"}}`, GetCollectionItemBranchFound},
		{"rejected", 401, `{"message":"SECRET SENTINEL"}`, GetCollectionItemBranchProviderRejected},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, "/v2/collections/62b720ef280c7a7a3be8cabe/items/62b720ef280c7a7a3be8cabe", r.URL.Path)
				require.Equal(t, "Bearer test-token", r.Header.Get("Authorization"))
				require.NotContains(t, r.URL.String(), "test-token")
				w.WriteHeader(test.status)
				_, err := fmt.Fprint(w, test.body)
				require.NoError(t, err)
			}))
			defer server.Close()
			reference := sdkgo.ConnectionRef{Provider: "webflow", Name: "webflow"}
			client, err := New(Config{BaseURL: server.URL + "/v2"}, sdkgo.StaticCredentialProvider[Credentials]{reference: {SiteToken: sdkgo.NewSecretString("test-token")}})
			require.NoError(t, err)
			result, err := sdkgo.RunQuery(testContext{}, client.GetCollectionItem(), reference, GetCollectionItemInput{CollectionID: "62b720ef280c7a7a3be8cabe", ItemID: "62b720ef280c7a7a3be8cabe"})
			require.NoError(t, err)
			require.Equal(t, test.branch, result.Branch)
			if test.name == "rejected" {
				require.NotNil(t, result.Failure)
				require.NotContains(t, result.Failure.Message, "SENTINEL")
			}
		})
	}
}
