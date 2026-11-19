// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package acrobatsign

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestCancelAgreementReadsStatusBeforeTransition(t *testing.T) {
	const agreementID = "3AAABLblqZNOTREALAGREEMENTID5_BjiH"
	for _, testCase := range []struct {
		name           string
		status         string
		wantTransition bool
	}{
		{name: "active", status: "OUT_FOR_SIGNATURE", wantTransition: true},
		{name: "already cancelled", status: "ABORTED"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var server *httptest.Server
			transitions := 0
			server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				require.Equal(t, "Bearer sample-acrobat-sign-integration-key", request.Header.Get("Authorization"))
				switch request.URL.Path {
				case "/api/rest/v6/baseUris":
					_, err := fmt.Fprintf(writer, `{"apiAccessPoint":%q}`, server.URL+"/")
					require.NoError(t, err)
				case "/api/rest/v6/agreements/" + agreementID:
					require.Equal(t, http.MethodGet, request.Method)
					_, err := fmt.Fprintf(writer, `{"id":%q,"status":%q,"participantSetsInfo":[{"email":"private@example.com"}]}`, agreementID, testCase.status)
					require.NoError(t, err)
				case "/api/rest/v6/agreements/" + agreementID + "/state":
					transitions++
					require.Equal(t, http.MethodPut, request.Method)
					body, err := io.ReadAll(request.Body)
					require.NoError(t, err)
					require.JSONEq(t, `{"state":"CANCELLED"}`, string(body))
					writer.WriteHeader(http.StatusNoContent)
				default:
					t.Fatalf("unexpected path %s", request.URL.Path)
				}
			}))
			defer server.Close()
			client := newTestClient(t, server.URL+"/api/rest/v6", 0)
			result, err := sdkgo.RunMutation(queryContext{}, client.CancelAgreement(), testConnection, CancelAgreementInput{AgreementID: agreementID})
			require.NoError(t, err)
			require.Equal(t, CancelAgreementBranchCancelled, result.Branch)
			require.Equal(t, CancelledAgreement{AgreementID: agreementID, Status: "CANCELLED"}, result.Value)
			if testCase.wantTransition {
				require.Equal(t, 1, transitions)
			} else {
				require.Zero(t, transitions)
			}
		})
	}
}

func TestCancelAgreementClassifiesReadFailures(t *testing.T) {
	const agreementID = "3AAABLblqZNOTREALAGREEMENTID5_BjiH"
	for _, testCase := range []struct {
		name   string
		status int
		body   string
		branch sdkgo.BranchID
		retry  bool
	}{
		{name: "missing", status: 404, body: `{}`, branch: CancelAgreementBranchNotFound},
		{name: "denied", status: 403, body: `{}`, branch: CancelAgreementBranchProviderRejected},
		{name: "invalid", status: 200, body: `{`, branch: CancelAgreementBranchInvalidResponse},
		{name: "rate limited", status: 429, body: `{}`, retry: true},
		{name: "unavailable", status: 503, body: `{}`, retry: true},
		{name: "unknown", status: 500, body: `{}`, retry: true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var server *httptest.Server
			server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/api/rest/v6/baseUris" {
					_, err := fmt.Fprintf(writer, `{"apiAccessPoint":%q}`, server.URL+"/")
					require.NoError(t, err)
					return
				}
				writer.WriteHeader(testCase.status)
				_, err := writer.Write([]byte(testCase.body))
				require.NoError(t, err)
			}))
			defer server.Close()
			client := newTestClient(t, server.URL+"/api/rest/v6", 0)
			result, err := sdkgo.RunMutation(queryContext{}, client.CancelAgreement(), testConnection, CancelAgreementInput{AgreementID: agreementID})
			if testCase.retry {
				var retry *sdkgo.RetryError
				require.ErrorAs(t, err, &retry)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testCase.branch, result.Branch)
		})
	}
}
