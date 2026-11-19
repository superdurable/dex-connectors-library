// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const createdDomainResponse = `{"IdentityType":"DOMAIN","VerifiedForSendingStatus":false,"DkimAttributes":{"Tokens":["token-a","token-b","token-c"]}}`

func TestCreateEmailIdentitySignsAndStartsVerification(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v2/email/identities", request.URL.Path)
		require.Contains(t, request.Header.Get("Authorization"), "/ses/aws4_request")
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.JSONEq(t, `{"EmailIdentity":"example.com"}`, string(body))
		_, err = response.Write([]byte(createdDomainResponse))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(&conformanceSendContext{attempt: 1}, newTestClient(t, server.URL, 0).CreateEmailIdentity(), testConnection, CreateEmailIdentityInput{EmailIdentity: "example.com"})
	require.NoError(t, err)
	require.Equal(t, CreateEmailIdentityBranchCreated, result.Branch)
	require.Equal(t, CreatedEmailIdentity{Name: "example.com", IdentityType: "DOMAIN", DKIMTokens: []string{"token-a", "token-b", "token-c"}}, result.Value)
}

func TestCreateEmailIdentityDoesNotConfirmByNameAfterUnknownOutcome(t *testing.T) {
	var sends, reads int
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Method == http.MethodPost {
			sends++
			response.WriteHeader(http.StatusInternalServerError)
			return
		}
		reads++
		require.Equal(t, "/v2/email/identities/example.com", request.URL.Path)
		_, err := response.Write([]byte(createdDomainResponse))
		require.NoError(t, err)
	}))
	defer server.Close()
	client := newTestClient(t, server.URL, 0)
	context := &conformanceSendContext{attempt: 1}
	_, err := sdkgo.RunMutation(context, client.CreateEmailIdentity(), testConnection, CreateEmailIdentityInput{EmailIdentity: "example.com"})
	var retry *sdkgo.RetryError
	require.ErrorAs(t, err, &retry)
	context.attempt = 2
	_, err = sdkgo.RunMutation(context, client.CreateEmailIdentity(), testConnection, CreateEmailIdentityInput{EmailIdentity: "example.com"})
	require.ErrorAs(t, err, &retry)
	require.Equal(t, 2, sends)
	require.Equal(t, 0, reads)
}

func TestCreateEmailIdentityClassifiesDefiniteAndRetryableOutcomes(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(status)
			if status == http.StatusBadRequest {
				_, err := response.Write([]byte(`{"__type":"BadRequestException","message":"bad request"}`))
				require.NoError(t, err)
			}
		}))
		result, err := sdkgo.RunMutation(&conformanceSendContext{attempt: 1}, newTestClient(t, server.URL, 0).CreateEmailIdentity(), testConnection, CreateEmailIdentityInput{EmailIdentity: "example.com"})
		if status == http.StatusBadRequest {
			require.NoError(t, err)
			require.Equal(t, CreateEmailIdentityBranchProviderRejected, result.Branch)
		} else {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
		}
		server.Close()
	}
}
