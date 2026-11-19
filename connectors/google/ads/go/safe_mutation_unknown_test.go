// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ads

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestSafeCampaignMutationRetriesMalformedSuccess(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusOK)
		_, err := response.Write([]byte(`{"results":[{`))
		require.NoError(t, err)
	}))
	defer provider.Close()
	client := newTestClient(t, provider.URL)
	_, err := sdkgo.RunMutation(queryContext{}, client.UpdateCampaignStatus(), testConnection,
		UpdateCampaignStatusInput{CustomerID: "1234567890", CampaignID: "1234567890", Status: "PAUSED"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}

func TestSafeCampaignMutationRetriesCredentialBearingResponse(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		_, err := response.Write([]byte(testAccessToken))
		require.NoError(t, err)
	}))
	defer provider.Close()
	client := newTestClient(t, provider.URL)
	_, err := sdkgo.RunMutation(queryContext{}, client.UpdateCampaignStatus(), testConnection,
		UpdateCampaignStatusInput{CustomerID: "1234567890", CampaignID: "1234567890", Status: "PAUSED"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
}
