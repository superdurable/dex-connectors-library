// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package freshsales

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatchtest"
)

func TestLimitedWriteRefusalRequiresProviderEvidence(t *testing.T) {
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		want   bool
	}{
		{name: "documented refusal", status: http.StatusBadRequest, body: `{"errors":{"code":"400","message":"Invalid request"}}`, want: true},
		{name: "empty body", status: http.StatusBadRequest},
		{name: "contradictory body", status: http.StatusBadRequest, body: `{"errors":{"code":"400","message":"Invalid request"},"id":1}`},
		{name: "conflict", status: http.StatusConflict, body: `{"errors":{"code":"400","message":"Invalid request"}}`},
		{name: "other client error", status: http.StatusMethodNotAllowed, body: `{"errors":{"code":"400","message":"Invalid request"}}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := isDefiniteFreshsalesRefusal(testCase.status, []byte(testCase.body)); got != testCase.want {
				t.Fatalf("isDefiniteFreshsalesRefusal(%d, %q) = %t, want %t", testCase.status, testCase.body, got, testCase.want)
			}
		})
	}
}

func TestSafeWritesRetryStatusWithoutRefusalEvidence(t *testing.T) {
	provider := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
		response.WriteHeader(http.StatusBadRequest)
	}))
	defer provider.Close()
	client := newTestClient(t, provider.URL)
	for _, testCase := range []struct {
		name string
		run  func(*singledispatchtest.Context) error
	}{
		{name: "update contact", run: func(ctx *singledispatchtest.Context) error {
			_, err := sdkgo.RunMutation(ctx, client.UpdateContact(), testConnection, UpdateContactInput{ContactID: "144", LastName: "Lovelace"})
			return err
		}},
		{name: "delete contact", run: func(ctx *singledispatchtest.Context) error {
			_, err := sdkgo.RunMutation(ctx, client.DeleteContact(), testConnection, DeleteContactInput{ContactID: "144"})
			return err
		}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, testCase.run(singledispatchtest.NewContext("freshsales-refusal-evidence", testCase.name)), &retry)
		})
	}
}
