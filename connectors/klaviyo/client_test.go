// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package klaviyo

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex/sdk-go/dex"
)

type providerContext struct{ dex.Context }

func (providerContext) FlowID() string              { return "klaviyo-provider-test" }
func (providerContext) StepExecutionID() string     { return "provider-step" }
func (providerContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (providerContext) Done() <-chan struct{}       { return nil }
func (providerContext) Err() error                  { return nil }
func (providerContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "klaviyo", Name: "klaviyo"}

func newProviderClient(t *testing.T, address string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: address + "/api"}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {PrivateAPIKey: sdkgo.NewSecretString("pk_test_key")}})
	require.NoError(t, err)
	return client
}

func TestGetProfileAndNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/profiles/profile-1", r.URL.Path)
		require.Equal(t, "Klaviyo-API-Key pk_test_key", r.Header.Get("Authorization"))
		require.Equal(t, apiRevision, r.Header.Get("revision"))
		require.Empty(t, r.URL.RawQuery)
		_, err := fmt.Fprint(w, `{"data":{"type":"profile","id":"profile-1","attributes":{"email":"member@meridian.co"}}}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(providerContext{}, newProviderClient(t, server.URL).GetProfile(), testConnection, GetProfileInput{ID: "profile-1"})
	require.NoError(t, err)
	require.Equal(t, GetProfileBranchFound, result.Branch)
	require.Equal(t, Profile{ID: "profile-1", Email: "member@meridian.co"}, result.Value)

	missing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNotFound) }))
	defer missing.Close()
	result, err = sdkgo.RunQuery(providerContext{}, newProviderClient(t, missing.URL).GetProfile(), testConnection, GetProfileInput{ID: "missing"})
	require.NoError(t, err)
	require.Equal(t, GetProfileBranchNotFound, result.Branch)
}

func TestCreateEventUsesStableUniqueIDAndAccepts202(t *testing.T) {
	var uniqueID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/api/events", r.URL.Path)
		require.Equal(t, "application/vnd.api+json", r.Header.Get("Content-Type"))
		var payload struct {
			Data struct {
				Attributes struct {
					UniqueID string `json:"unique_id"`
					Metric   struct {
						Data struct {
							Attributes struct {
								Name string `json:"name"`
							} `json:"attributes"`
						} `json:"data"`
					} `json:"metric"`
				} `json:"attributes"`
			} `json:"data"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Equal(t, "Signed Up", payload.Data.Attributes.Metric.Data.Attributes.Name)
		require.Len(t, payload.Data.Attributes.UniqueID, 64)
		if uniqueID != "" {
			require.Equal(t, uniqueID, payload.Data.Attributes.UniqueID)
		}
		uniqueID = payload.Data.Attributes.UniqueID
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	client := newProviderClient(t, server.URL)
	input := CreateEventInput{Email: "member@meridian.co", MetricName: "Signed Up"}
	for range 2 {
		result, err := sdkgo.RunMutation(providerContext{}, client.CreateEvent(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, CreateEventBranchAccepted, result.Branch)
		require.True(t, result.Value.Accepted)
	}
}

func TestCreateEventRejectsBadInputBeforeDispatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected provider request") }))
	defer server.Close()
	result, err := sdkgo.RunMutation(providerContext{}, newProviderClient(t, server.URL).CreateEvent(), testConnection, CreateEventInput{Email: "bad", MetricName: "Signed Up"})
	require.NoError(t, err)
	require.Equal(t, CreateEventBranchDefect, result.Branch)
}
