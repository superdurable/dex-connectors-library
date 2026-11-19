// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package elevenlabs

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestDeleteHistoryItemRetriesUnknownOutcomes(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "conflict", status: http.StatusConflict, body: `{"detail":"conflict"}`},
		{name: "unexpected client status", status: http.StatusMethodNotAllowed, body: `{"detail":"method"}`},
		{name: "empty refusal", status: http.StatusBadRequest, body: `{}`},
		{name: "malformed success", status: http.StatusOK, body: `{}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			_, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL).DeleteHistoryItem(), testConnection, HistoryItemIdentityInput{HistoryItemID: "history_meridian"})
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		})
	}
}
