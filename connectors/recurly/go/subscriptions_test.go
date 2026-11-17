// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package recurly

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

const testSubscriptionID = "e28zov4fw0v2"

func TestSubscriptionQueriesExcludeAccountAndPaymentDetails(t *testing.T) {
	summary := `{"id":"` + testSubscriptionID + `","state":"active","account":{"email":"private@example.com"},"billing_info":{"number":"private"}}`
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path == "/api/subscriptions" {
			require.Equal(t, "2", request.URL.Query().Get("limit"))
			_, err := writer.Write([]byte(`{"object":"list","has_more":false,"data":[` + summary + `]}`))
			require.NoError(t, err)
			return
		}
		require.Equal(t, "/api/subscriptions/"+testSubscriptionID, request.URL.Path)
		_, err := writer.Write([]byte(summary))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api", 0)
	listed, err := sdkgo.RunQuery(queryContext{}, client.ListSubscriptions(), testConnection, ListSubscriptionsInput{Limit: 2})
	require.NoError(t, err)
	require.Equal(t, ListSubscriptionsBranchListed, listed.Branch)
	found, err := sdkgo.RunQuery(queryContext{}, client.GetSubscription(), testConnection, GetSubscriptionInput{SubscriptionID: testSubscriptionID})
	require.NoError(t, err)
	require.Equal(t, GetSubscriptionBranchFound, found.Branch)
	encoded, err := json.Marshal(found.Value)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private")
}

func TestCancelSubscriptionConfirmsRepeatedRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPut {
			writer.WriteHeader(http.StatusUnprocessableEntity)
			return
		}
		require.Equal(t, "/api/subscriptions/"+testSubscriptionID, request.URL.Path)
		_, err := writer.Write([]byte(`{"id":"` + testSubscriptionID + `","state":"canceled"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL+"/api", 0)
	cancelled, err := sdkgo.RunMutation(singledispatchtest.NewContext("recurly-subscription", "cancel"), client.CancelSubscription(), testConnection, CancelSubscriptionInput{SubscriptionID: testSubscriptionID})
	require.NoError(t, err)
	require.Equal(t, CancelSubscriptionBranchCancelled, cancelled.Branch)
}
