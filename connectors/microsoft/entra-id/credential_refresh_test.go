// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/entra-id/internal/graphfake"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

var refreshTestNow = time.Date(2026, time.October, 1, 8, 0, 0, 0, time.UTC)

func newFakeTokenDriver(t *testing.T) (*CredentialRefreshDriver, *graphfake.Server) {
	t.Helper()
	tokenServer := graphfake.New(t.Cleanup)
	routed, err := newLocalProviderHTTPClient(&http.Client{Timeout: 5 * time.Second}, tokenServer.URL)
	require.NoError(t, err)
	driver := NewCredentialRefreshDriver(routed)
	driver.now = func() time.Time { return refreshTestNow }
	return driver, tokenServer
}

func appOnlyCredentials() Credentials {
	return Credentials{
		AuthMethodID: EntraAppOnlyAuthMethodID, TenantID: graphfake.TenantID,
		ClientID: graphfake.ClientID, ClientSecret: sdkgo.NewSecretString(graphfake.ClientSecret),
	}
}

func TestAppOnlyRefreshUsesTheClientCredentialsGrantForTheTenant(t *testing.T) {
	driver, tokenServer := newFakeTokenDriver(t)

	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: appOnlyCredentials()})
	require.NoError(t, err)
	require.Equal(t, graphfake.AccessToken, result.Credentials.AccessToken.Reveal())
	require.Equal(t, refreshTestNow.Add(3599*time.Second), result.ExpiresAt)
	require.Equal(t, graphfake.ClientSecret, result.Credentials.ClientSecret.Reveal(), "the client secret is kept for the next exchange")
	request := tokenServer.Requests()[0]
	require.Equal(t, "/"+graphfake.TenantID+"/oauth2/v2.0/token", request.Path)
	require.Equal(t, "client_credentials", request.Form.Get("grant_type"))
	require.Equal(t, "https://graph.microsoft.com/.default", request.Form.Get("scope"))
}

func TestAppOnlyRefreshRejectsInvalidTenantsAndClientsWithReauthorization(t *testing.T) {
	driver, tokenServer := newFakeTokenDriver(t)
	for _, tenantID := range []string{"", "common", "Organizations", "consumers", "contoso", "contoso.com/../x", "a b.com"} {
		credentials := appOnlyCredentials()
		credentials.TenantID = tenantID
		_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials})
		require.True(t, sdkgo.IsReauthorizationRequired(err), tenantID)
	}
	require.Empty(t, tokenServer.Requests(), "an invalid tenant never becomes a URL")

	domainTenant := appOnlyCredentials()
	domainTenant.TenantID = "Contoso.OnMicrosoft.com"
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: domainTenant})
	require.True(t, sdkgo.IsReauthorizationRequired(err), "the fake rejects any tenant but its own with invalid_client")
	require.Equal(t, "/contoso.onmicrosoft.com/oauth2/v2.0/token", tokenServer.Requests()[0].Path)

	wrongSecret := appOnlyCredentials()
	wrongSecret.ClientSecret = sdkgo.NewSecretString("wrong")
	_, err = driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: wrongSecret})
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), graphfake.MessageSentinel)
}

func TestMicrosoftOAuthRefreshRotatesTheRefreshTokenAndChecksPermissions(t *testing.T) {
	driver, tokenServer := newFakeTokenDriver(t)
	credentials := Credentials{
		AuthMethodID: MicrosoftOAuthAuthMethodID, ClientID: graphfake.ClientID, ClientSecret: sdkgo.NewSecretString(graphfake.ClientSecret),
		AccessToken: sdkgo.NewSecretString("expired"), RefreshToken: sdkgo.NewSecretString(graphfake.RefreshToken),
	}
	for _, grantedScope := range []string{
		"User.Read.All User.Create User.EnableDisableAccount.All User.RevokeSessions.All GroupMember.ReadWrite.All",
		"https://graph.microsoft.com/user.read.all https://graph.microsoft.com/User.Create https://graph.microsoft.com/User.EnableDisableAccount.All " +
			"https://graph.microsoft.com/User.RevokeSessions.All https://graph.microsoft.com/GroupMember.ReadWrite.All openid",
		"",
	} {
		tokenServer.GrantedScope = grantedScope
		result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials})
		require.NoError(t, err, grantedScope)
		require.Equal(t, graphfake.AccessToken, result.Credentials.AccessToken.Reveal())
		require.Equal(t, graphfake.RefreshToken, result.Credentials.RefreshToken.Reveal())
	}
	request := tokenServer.Requests()[0]
	require.Equal(t, "/organizations/oauth2/v2.0/token", request.Path)
	require.Equal(t, "refresh_token", request.Form.Get("grant_type"))
	require.Equal(t, "offline_access User.Read.All User.Create User.EnableDisableAccount.All User.RevokeSessions.All GroupMember.ReadWrite.All", request.Form.Get("scope"))

	tokenServer.GrantedScope = "User.Read.All GroupMember.ReadWrite.All"
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials})
	require.True(t, sdkgo.IsReauthorizationRequired(err), "a token without every Graph permission needs new consent")

	credentials.RefreshToken = sdkgo.NewSecretString("revoked")
	_, err = driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: credentials})
	require.True(t, sdkgo.IsReauthorizationRequired(err), "invalid_grant is terminal")
}

func TestRefreshRetriesMicrosoftOutagesWithoutReauthorization(t *testing.T) {
	driver, tokenServer := newFakeTokenDriver(t)
	tokenServer.Close()

	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: appOnlyCredentials()})
	require.Error(t, err)
	require.False(t, sdkgo.IsReauthorizationRequired(err))

	_, err = driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{AuthMethodID: "unknown"}})
	require.True(t, sdkgo.IsReauthorizationRequired(err))
}

func TestRefreshRequiredForMissingOrNearExpiry(t *testing.T) {
	driver := NewCredentialRefreshDriver(nil)
	soon, later := refreshTestNow.Add(time.Minute), refreshTestNow.Add(time.Hour)
	withToken := Credentials{AuthMethodID: EntraAppOnlyAuthMethodID, AccessToken: sdkgo.NewSecretString("token")}
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: Credentials{}, Now: refreshTestNow}))
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: withToken, Now: refreshTestNow}))
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: withToken, ExpiresAt: &soon, Now: refreshTestNow}))
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[Credentials]{Credentials: withToken, ExpiresAt: &later, Now: refreshTestNow}))
}
