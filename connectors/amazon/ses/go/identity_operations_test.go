// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func TestListEmailIdentitiesSignsAndValidatesPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v2/email/list-identities", request.URL.Path)
		require.Contains(t, request.Header.Get("Authorization"), "/ses/aws4_request")
		_, err := response.Write([]byte(`{"EmailIdentities":[{"IdentityName":"example.com","IdentityType":"DOMAIN","VerificationStatus":"SUCCESS","SendingEnabled":true}],"NextToken":"more"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListEmailIdentities(), testConnection, ListEmailIdentitiesInput{PageSize: 5})
	require.NoError(t, err)
	require.Equal(t, ListEmailIdentitiesBranchPage, result.Branch)
	require.Equal(t, "more", result.Value.NextToken)
	require.Equal(t, []EmailIdentitySummary{{Name: "example.com", IdentityType: "DOMAIN", VerificationStatus: "SUCCESS", SendingEnabled: true}}, result.Value.Identities)
}

func TestListEmailIdentitiesClassifiesRejectionAndUnknownOutcome(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(status)
			message := `{"message":"temporarily unavailable"}`
			if status == http.StatusForbidden {
				message = `{"message":"ses-test-secret"}`
			}
			_, err := response.Write([]byte(message))
			require.NoError(t, err)
		}))
		result, err := sdkgo.RunQuery(queryContext{}, newTestClient(t, server.URL, 0).ListEmailIdentities(), testConnection, ListEmailIdentitiesInput{})
		if status == http.StatusForbidden {
			require.NoError(t, err)
			require.Equal(t, ListEmailIdentitiesBranchDefect, result.Branch)
		} else {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		}
		server.Close()
	}
}

func TestDeleteEmailIdentitySignsAndClassifiesOutcomes(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNotFound, http.StatusForbidden, http.StatusTooManyRequests, http.StatusServiceUnavailable} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
			require.Equal(t, http.MethodDelete, request.Method)
			require.Equal(t, "/v2/email/identities/example.com", request.URL.Path)
			require.True(t, strings.Contains(request.Header.Get("Authorization"), "/ses/aws4_request"))
			response.WriteHeader(status)
		}))
		result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).DeleteEmailIdentity(), testConnection, DeleteEmailIdentityInput{EmailIdentity: "example.com"})
		switch status {
		case http.StatusOK, http.StatusNotFound:
			require.NoError(t, err)
			require.Equal(t, DeleteEmailIdentityBranchDeleted, result.Branch)
		case http.StatusForbidden:
			require.NoError(t, err)
			require.Equal(t, DeleteEmailIdentityBranchProviderRejected, result.Branch)
		default:
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		}
		server.Close()
	}
}
