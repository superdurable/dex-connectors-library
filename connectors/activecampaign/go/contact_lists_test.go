// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListContactMembershipsFiltersProviderFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/3/contactLists", r.URL.Path)
		require.Equal(t, "68", r.URL.Query().Get("contact"))
		require.Equal(t, "2", r.URL.Query().Get("limit"))
		require.Equal(t, "3", r.URL.Query().Get("offset"))
		_, err := w.Write([]byte(`{"contactLists":[{"id":"42","contact":"68","list":"11","status":1,"unsubreason":"private"}],"meta":{"total":"4"}}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	result, err := sdkgo.RunQuery(queryContext{}, client.ListContactListMemberships(), testConnection, ListContactListMembershipsInput{ContactID: "68", Limit: 2, Offset: 3})
	require.NoError(t, err)
	require.Equal(t, ListContactListMembershipsBranchListed, result.Branch)
	require.Equal(t, 4, result.Value.Total)
	require.Equal(t, ContactListMembership{ID: "42", ContactID: "68", ListID: "11", Status: 1}, result.Value.Memberships[0])
}

func TestSubscribeReadsBackAfterUnknownOutcome(t *testing.T) {
	var sends, reads atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			require.Equal(t, "68", r.URL.Query().Get("contact"))
			if reads.Add(1) == 1 {
				_, err := w.Write([]byte(`{"contactLists":[],"meta":{"total":"0"}}`))
				require.NoError(t, err)
			} else {
				_, err := w.Write([]byte(`{"contactLists":[{"id":"42","contact":"68","list":"11","status":1}],"meta":{"total":"1"}}`))
				require.NoError(t, err)
			}
			return
		}
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/3/contactLists", r.URL.Path)
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"contactList":{"contact":"68","list":"11","status":1}}`, string(body))
		sends.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	ctx := &dealRetryContext{attempt: 1}
	input := SetContactListMembershipInput{ContactID: "68", ListID: "11"}
	_, err := sdkgo.RunMutation(ctx, client.SubscribeContactToList(), testConnection, input)
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	ctx.attempt = 2
	result, err := sdkgo.RunMutation(ctx, client.SubscribeContactToList(), testConnection, input)
	require.NoError(t, err)
	require.Equal(t, SubscribeContactToListBranchSubscribed, result.Branch)
	require.Equal(t, 1, result.Value.Status)
	require.Equal(t, int32(1), sends.Load())
	require.Equal(t, int32(2), reads.Load())
}

func TestUnsubscribeConvergesWithoutAnotherWrite(t *testing.T) {
	var sends atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, err := w.Write([]byte(`{"contactLists":[{"id":"42","contact":"68","list":"11","status":2}],"meta":{"total":"1"}}`))
			require.NoError(t, err)
			return
		}
		sends.Add(1)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api/3")
	result, err := sdkgo.RunMutation(queryContext{}, client.UnsubscribeContactFromList(), testConnection, SetContactListMembershipInput{ContactID: "68", ListID: "11"})
	require.NoError(t, err)
	require.Equal(t, UnsubscribeContactFromListBranchUnsubscribed, result.Branch)
	require.Equal(t, 2, result.Value.Status)
	require.Zero(t, sends.Load())
}
