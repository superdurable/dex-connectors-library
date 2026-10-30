// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mixpanel

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

func (providerContext) FlowID() string              { return "mixpanel-provider-test" }
func (providerContext) StepExecutionID() string     { return "provider-step" }
func (providerContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (providerContext) Done() <-chan struct{}       { return nil }
func (providerContext) Err() error                  { return nil }
func (providerContext) Value(key any) any           { return context.Background().Value(key) }

var testConnection = sdkgo.ConnectionRef{Provider: "mixpanel", Name: "mixpanel"}

func newProviderClient(t *testing.T, address string) *Client {
	t.Helper()
	client, err := New(Config{BaseURL: address, ExportBaseURL: address + "/api/2.0", ProjectID: 12345}, sdkgo.StaticCredentialProvider[Credentials]{testConnection: {ServiceAccountUsername: sdkgo.NewSecretString("service-user"), ServiceAccountSecret: sdkgo.NewSecretString("service-secret")}})
	require.NoError(t, err)
	return client
}

func TestTrackEventSendsStableDeduplicationTuple(t *testing.T) {
	var insertID string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "POST", r.Method)
		require.Equal(t, "/import", r.URL.Path)
		require.Equal(t, "12345", r.URL.Query().Get("project_id"))
		require.Equal(t, "1", r.URL.Query().Get("strict"))
		username, secret, ok := r.BasicAuth()
		require.True(t, ok)
		require.Equal(t, "service-user", username)
		require.Equal(t, "service-secret", secret)
		require.NotContains(t, r.URL.String(), "service-secret")
		var payload []struct {
			Event      string `json:"event"`
			Properties struct {
				Time       int64  `json:"time"`
				DistinctID string `json:"distinct_id"`
				InsertID   string `json:"$insert_id"`
			} `json:"properties"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		require.Len(t, payload, 1)
		require.Equal(t, int64(1793232000), payload[0].Properties.Time)
		require.Equal(t, "member-1", payload[0].Properties.DistinctID)
		require.Len(t, payload[0].Properties.InsertID, 36)
		if insertID != "" {
			require.Equal(t, insertID, payload[0].Properties.InsertID)
		}
		insertID = payload[0].Properties.InsertID
		_, err := fmt.Fprint(w, `{"code":200,"num_records_imported":1,"status":"OK"}`)
		require.NoError(t, err)
	}))
	defer server.Close()
	input := TrackEventInput{Event: "Signed Up", DistinctID: "member-1", Time: 1793232000}
	for range 2 {
		result, err := sdkgo.RunMutation(providerContext{}, newProviderClient(t, server.URL).TrackEvent(), testConnection, input)
		require.NoError(t, err)
		require.Equal(t, TrackEventBranchImported, result.Branch)
		require.True(t, result.Value.Imported)
	}
}

func TestTrackEventRejectsInvalidInputBeforeDispatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected provider request") }))
	defer server.Close()
	result, err := sdkgo.RunMutation(providerContext{}, newProviderClient(t, server.URL).TrackEvent(), testConnection, TrackEventInput{Event: "Signed Up", Time: 1793232000})
	require.NoError(t, err)
	require.Equal(t, TrackEventBranchDefect, result.Branch)
}

func TestTrackEventClassifiesProviderRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(providerContext{}, newProviderClient(t, server.URL).TrackEvent(), testConnection, TrackEventInput{Event: "Signed Up", DistinctID: "member-1", Time: 1793232000})
	require.NoError(t, err)
	require.Equal(t, TrackEventBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
}

func TestExportEventsDecodesJSONLWithoutArbitraryProperties(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "GET", r.Method)
		require.Equal(t, "/api/2.0/export", r.URL.Path)
		require.Equal(t, "2026-10-28", r.URL.Query().Get("from_date"))
		require.Equal(t, "2026-10-28", r.URL.Query().Get("to_date"))
		require.Equal(t, "10", r.URL.Query().Get("limit"))
		_, err := fmt.Fprint(w, "{\"event\":\"Signed Up\",\"properties\":{\"time\":1793232000,\"distinct_id\":\"member-1\",\"$insert_id\":\"stable-id\",\"password\":\"SENTINEL\"}}\n")
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(providerContext{}, newProviderClient(t, server.URL).ExportEvents(), testConnection, ExportEventsInput{FromDate: "2026-10-28", ToDate: "2026-10-28", Limit: 10})
	require.NoError(t, err)
	require.Equal(t, ExportEventsBranchExported, result.Branch)
	require.Equal(t, []ExportedEvent{{Event: "Signed Up", DistinctID: "member-1", Time: 1793232000, InsertID: "stable-id"}}, result.Value.Events)
	encoded, err := json.Marshal(result.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "SENTINEL")
}

func TestExportEventsRejectsInvalidDateBeforeDispatch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("unexpected provider request") }))
	defer server.Close()
	result, err := sdkgo.RunQuery(providerContext{}, newProviderClient(t, server.URL).ExportEvents(), testConnection, ExportEventsInput{FromDate: "bad", ToDate: "2026-10-28", Limit: 10})
	require.NoError(t, err)
	require.Equal(t, ExportEventsBranchDefect, result.Branch)
}

func TestExportEventsClassifiesProviderRejection(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(providerContext{}, newProviderClient(t, server.URL).ExportEvents(), testConnection, ExportEventsInput{FromDate: "2026-10-28", ToDate: "2026-10-28", Limit: 10})
	require.NoError(t, err)
	require.Equal(t, ExportEventsBranchProviderRejected, result.Branch)
	require.Equal(t, sdkgo.FailureAuthentication, result.Failure.Kind)
}
