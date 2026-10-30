// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docusign_test

import (
	"context"
	"encoding/base64"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/docusign"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

func refreshState(credentials docusign.Credentials, expiresAt *time.Time) sdkgo.CredentialRefreshState[docusign.Credentials] {
	return sdkgo.CredentialRefreshState[docusign.Credentials]{Credentials: credentials, ExpiresAt: expiresAt, Now: time.Now()}
}

func TestRefreshUsesTheEnvironmentsTokenEndpointWithBasicClientAuthentication(t *testing.T) {
	for authMethodID, expectedHost := range map[string]string{
		docusign.ProductionOAuthAuthMethodID: "account.docusign.com",
		docusign.DeveloperOAuthAuthMethodID:  "account-d.docusign.com",
	} {
		t.Run(authMethodID, func(t *testing.T) {
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
				"POST /oauth/token": respondJSON(http.StatusOK, `{"access_token":"new-access","token_type":"Bearer","refresh_token":"new-refresh","expires_in":28800}`),
			})
			credentials := productionCredentials()
			credentials.AuthMethodID = authMethodID
			driver := docusign.NewCredentialRefreshDriver(fake.redirectingClient())
			before := time.Now()
			result, err := driver.Refresh(context.Background(), refreshState(credentials, nil))
			require.NoError(t, err)
			require.Equal(t, "new-access", result.Credentials.AccessToken.Reveal())
			require.Equal(t, "new-refresh", result.Credentials.RefreshToken.Reveal())
			require.Equal(t, sentinelHMACKey, result.Credentials.ConnectHMACKey.Reveal(), "the Connect key survives a refresh")
			require.WithinDuration(t, before.Add(8*time.Hour), result.ExpiresAt, time.Minute)

			request := fake.requestsTo(http.MethodPost, "/oauth/token")[0]
			require.Equal(t, expectedHost, request.host)
			require.Equal(t, "Basic "+base64.StdEncoding.EncodeToString([]byte("integration-key:"+sentinelClientSecret)), request.authorization)
			require.Contains(t, request.body, "grant_type=refresh_token")
			require.NotContains(t, request.body, sentinelClientSecret, "the secret travels only in the Basic header")
		})
	}
}

func TestRefreshKeepsTheRefreshTokenDocuSignOmits(t *testing.T) {
	fake := newFakeDocuSign(t, map[string]http.HandlerFunc{
		"POST /oauth/token": respondJSON(http.StatusOK, `{"access_token":"new-access","token_type":"Bearer","expires_in":28800}`),
	})
	result, err := docusign.NewCredentialRefreshDriver(fake.redirectingClient()).Refresh(context.Background(), refreshState(productionCredentials(), nil))
	require.NoError(t, err)
	require.Equal(t, sentinelRefreshToken, result.Credentials.RefreshToken.Reveal())
}

func TestRefreshRequiresReauthorizationForARejectedGrantButRetriesAnOutage(t *testing.T) {
	for name, test := range map[string]struct {
		status              int
		body                string
		isReauthorization   bool
		authMethodIDForTest string
	}{
		"expired refresh token": {http.StatusBadRequest, `{"error":"invalid_grant","error_description":"` + sentinelMessage + `"}`, true, docusign.ProductionOAuthAuthMethodID},
		"revoked integration":   {http.StatusUnauthorized, `{"error":"invalid_client"}`, true, docusign.ProductionOAuthAuthMethodID},
		"outage":                {http.StatusServiceUnavailable, `{"error":"invalid_grant"}`, false, docusign.ProductionOAuthAuthMethodID},
		"unknown environment":   {http.StatusOK, `{}`, true, "unknown"},
	} {
		t.Run(name, func(t *testing.T) {
			fake := newFakeDocuSign(t, map[string]http.HandlerFunc{"POST /oauth/token": respondJSON(test.status, test.body)})
			credentials := productionCredentials()
			credentials.AuthMethodID = test.authMethodIDForTest
			_, err := docusign.NewCredentialRefreshDriver(fake.redirectingClient()).Refresh(context.Background(), refreshState(credentials, nil))
			require.Error(t, err)
			require.Equal(t, test.isReauthorization, sdkgo.IsReauthorizationRequired(err))
			require.NotContains(t, err.Error(), sentinelMessage)
			require.NotContains(t, err.Error(), sentinelRefreshToken)
		})
	}
}

func TestRefreshIsRequiredWithinFiveMinutesOfExpiryOrWithoutOne(t *testing.T) {
	driver := docusign.NewCredentialRefreshDriver(nil)
	soon, later := time.Now().Add(4*time.Minute), time.Now().Add(time.Hour)
	require.True(t, driver.RefreshRequired(refreshState(productionCredentials(), nil)))
	require.True(t, driver.RefreshRequired(refreshState(productionCredentials(), &soon)))
	require.False(t, driver.RefreshRequired(refreshState(productionCredentials(), &later)))
	withoutToken := productionCredentials()
	withoutToken.AccessToken = sdkgo.NewSecretString("")
	require.True(t, driver.RefreshRequired(refreshState(withoutToken, &later)))
}
