// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

type dealRetryContext struct {
	queryContext
	attempt   int32
	heartbeat []byte
}

func (ctx *dealRetryContext) Attempt() int32 { return ctx.attempt }
func (ctx *dealRetryContext) RecordHeartbeat(value any) error {
	encoded, err := json.Marshal(value)
	if err != nil {
		return err
	}
	ctx.heartbeat = encoded
	return nil
}
func (ctx *dealRetryContext) GetLastHeartbeatValue(value any) (bool, error) {
	if len(ctx.heartbeat) == 0 {
		return false, nil
	}
	return true, json.Unmarshal(ctx.heartbeat, value)
}

func TestDealQueriesExcludeRelationships(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, testAPIKey, r.Header.Get("Api-Token"))
		switch r.URL.Path {
		case "/api/3/deals":
			require.Equal(t, "2", r.URL.Query().Get("limit"))
			require.Equal(t, "3", r.URL.Query().Get("offset"))
			_, err := w.Write([]byte(`{"deals":[{"id":"46","title":"Renewal","contact":"68","stage":"2","value":"45600","currency":"usd","status":"0","links":{"notes":"private-url"}}],"meta":{"total":12}}`))
			require.NoError(t, err)
		case "/api/3/deals/46":
			_, err := w.Write([]byte(`{"deal":{"id":"46","title":"Renewal","contact":"68","stage":"2","value":"45600","currency":"usd","status":"0"},"contacts":[{"email":"private@example.com"}]}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected path %s", r.URL.Path)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	page, err := sdkgo.RunQuery(queryContext{}, client.ListDeals(), testConnection, ListDealsInput{Limit: 2, Offset: 3})
	require.NoError(t, err)
	require.Equal(t, ListDealsBranchListed, page.Branch)
	require.Equal(t, 12, page.Value.Total)
	deal, err := sdkgo.RunQuery(queryContext{}, client.GetDeal(), testConnection, GetDealInput{DealID: "46"})
	require.NoError(t, err)
	require.Equal(t, GetDealBranchFound, deal.Branch)
	require.Equal(t, page.Value.Deals[0], deal.Value)
	encoded, err := json.Marshal(deal.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private@example.com")
}

func TestCreateDealReadsBackAfterUnknownOutcome(t *testing.T) {
	var sends, reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			require.Equal(t, "/api/3/deals", r.URL.Path)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"deal":{"title":"Renewal","contact":"68","group":"1","stage":"2","value":45600,"currency":"usd","status":0}}`, string(body))
			sends.Add(1)
			w.WriteHeader(http.StatusInternalServerError)
		case http.MethodGet:
			require.Equal(t, "/api/3/deals", r.URL.Path)
			require.Equal(t, "Renewal", r.URL.Query().Get("filters[title]"))
			reads.Add(1)
			_, err := w.Write([]byte(`{"deals":[{"id":"46","title":"Renewal","contact":"68","stage":"2","value":"45600","currency":"usd","status":"0"}],"meta":{"total":1}}`))
			require.NoError(t, err)
		default:
			t.Errorf("unexpected method %s", r.Method)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	ctx := &dealRetryContext{attempt: 1}
	input := CreateDealInput{Title: "Renewal", ContactID: "68", PipelineID: "1", StageID: "2", ValueMinor: 45600, Currency: "USD"}
	_, err := sdkgo.RunMutation(ctx, client.CreateDeal(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.attempt = 2
	created, err := sdkgo.RunMutation(ctx, client.CreateDeal(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, CreateDealBranchCreated, created.Branch)
	require.Equal(t, "46", created.Value.ID)
	require.Equal(t, int32(1), sends.Load())
	require.Equal(t, int32(1), reads.Load())
}

func TestDealUpdateAndDeleteConverge(t *testing.T) {
	var deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			require.Equal(t, "/api/3/deals/46", r.URL.Path)
			body, err := io.ReadAll(r.Body)
			require.NoError(t, err)
			require.JSONEq(t, `{"deal":{"status":1}}`, string(body))
			_, err = w.Write([]byte(`{"deal":{"id":"46","title":"Renewal","status":"1"}}`))
			require.NoError(t, err)
			return
		}
		require.Equal(t, http.MethodDelete, r.Method)
		require.Equal(t, "/api/3/deals/46", r.URL.Path)
		if deletes.Add(1) == 1 {
			_, err := w.Write([]byte(`{}`))
			require.NoError(t, err)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	status := 1
	updated, err := sdkgo.RunMutation(queryContext{}, client.UpdateDeal(), testConnection, UpdateDealInput{DealID: "46", Status: &status})
	require.NoError(t, err)
	require.Equal(t, UpdateDealBranchUpdated, updated.Branch)
	for range 2 {
		deleted, err := sdkgo.RunMutation(queryContext{}, client.DeleteDeal(), testConnection, DeleteDealInput{DealID: "46"})
		require.NoError(t, err)
		require.Equal(t, DeleteDealBranchDeleted, deleted.Branch)
	}
	require.Equal(t, int32(2), deletes.Load())
}
