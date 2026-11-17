// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package ses

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var exampleEmail = SendEmailInput{FromEmailAddress: "sender@example.com", ToEmailAddress: "reader@example.net", Subject: "Report ready", TextBody: "The report is ready."}

func TestSendEmailSignsAndAcceptsSimpleText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		require.Equal(t, http.MethodPost, request.Method)
		require.Equal(t, "/v2/email/outbound-emails", request.URL.Path)
		require.Contains(t, request.Header.Get("Authorization"), "/ses/aws4_request")
		body, err := io.ReadAll(request.Body)
		require.NoError(t, err)
		require.Contains(t, string(body), `"ToAddresses":["reader@example.net"]`)
		_, err = response.Write([]byte(`{"MessageId":"message-123"}`))
		require.NoError(t, err)
	}))
	defer server.Close()
	result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).SendEmail(), testConnection, exampleEmail)
	require.NoError(t, err)
	require.Equal(t, SendEmailBranchSent, result.Branch)
	require.Equal(t, "message-123", result.Value.MessageID)
}

func TestSendEmailClassifiesDefiniteAndUnknownOutcomes(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusTooManyRequests, http.StatusServiceUnavailable, http.StatusInternalServerError} {
		server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, _ *http.Request) {
			response.WriteHeader(status)
			_, err := response.Write([]byte(`{"message":"rejected"}`))
			require.NoError(t, err)
		}))
		result, err := sdkgo.RunMutation(queryContext{}, newTestClient(t, server.URL, 0).SendEmail(), testConnection, exampleEmail)
		if status == http.StatusBadRequest {
			require.NoError(t, err)
			require.Equal(t, SendEmailBranchProviderRejected, result.Branch)
		} else {
			var retry *sdkgo.RetryError
			require.ErrorAs(t, err, &retry)
			require.False(t, strings.Contains(retry.Error(), "ses-test-secret"))
		}
		server.Close()
	}
}
