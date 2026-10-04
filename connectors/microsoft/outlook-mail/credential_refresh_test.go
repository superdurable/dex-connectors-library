// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail_test

import (
	"context"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	outlookmail "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail/internal/graphtest"
	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// refreshDriverFor routes the driver's token requests to the fake through the connector's local transport.
func refreshDriverFor(t *testing.T, fake *graphtest.Server) *outlookmail.CredentialRefreshDriver {
	t.Helper()
	target, err := url.Parse(fake.URL)
	require.NoError(t, err)
	return outlookmail.NewCredentialRefreshDriver(&http.Client{Transport: rewritingTransport{target: target}})
}

// rewritingTransport sends https://login.microsoftonline.com requests to the fake, as WithLocalProviderURL does.
type rewritingTransport struct{ target *url.URL }

func (transport rewritingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	rewritten := request.Clone(request.Context())
	rewritten.URL.Scheme, rewritten.URL.Host, rewritten.Host = transport.target.Scheme, transport.target.Host, transport.target.Host
	return http.DefaultTransport.RoundTrip(rewritten)
}

func delegatedCredentials() outlookmail.Credentials {
	return outlookmail.Credentials{
		AuthMethodID: outlookmail.MicrosoftOAuthAuthMethodID, ClientID: testClientID(), ClientSecret: sdkgo.NewSecretString(testClientSecret),
		AccessToken: sdkgo.NewSecretString("expired-access-token"), RefreshToken: sdkgo.NewSecretString(testRefreshToken),
	}
}

func TestDelegatedRefreshRotatesTheRefreshTokenAndRepeatsTheScopes(t *testing.T) {
	fake := newGraphFake(t)
	driver := refreshDriverFor(t, fake)
	before := time.Now()
	result, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[outlookmail.Credentials]{Credentials: delegatedCredentials(), Now: before})
	require.NoError(t, err)
	require.NotEqual(t, "expired-access-token", result.Credentials.AccessToken.Reveal())
	require.Equal(t, fake.CurrentRefreshToken(), result.Credentials.RefreshToken.Reveal())
	require.WithinDuration(t, before.Add(3599*time.Second), result.ExpiresAt, 5*time.Second)
	request := fake.Requests(graphtest.EndpointDelegatedToken)[0]
	require.Equal(t, "/organizations/oauth2/v2.0/token", request.Path)
	form, err := url.ParseQuery(string(request.Body))
	require.NoError(t, err)
	require.Equal(t, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {testRefreshToken}, "client_id": {testClientID()},
		"client_secret": {testClientSecret}, "scope": {"offline_access Mail.ReadWrite Mail.Send"},
	}, form, "the secret travels in the form body, as Microsoft documents")
}

func TestDelegatedRefreshAcceptsResourcePrefixedScopesAndNeverRequiresOfflineAccess(t *testing.T) {
	for _, test := range []struct {
		scope         string
		isReauthorize bool
	}{
		{scope: "Mail.ReadWrite Mail.Send User.Read"},
		{scope: "https://graph.microsoft.com/Mail.ReadWrite https://graph.microsoft.com/Mail.Send"},
		{scope: "mail.readwrite MAIL.SEND"},
		{scope: "Mail.ReadWrite", isReauthorize: true},
		{scope: "Mail.Read Mail.Send offline_access", isReauthorize: true},
	} {
		t.Run(test.scope, func(t *testing.T) {
			fake := graphtest.Start(t, graphtest.ServerConfig{
				MailboxAddress: testMailbox, ClientID: testClientID(), ClientSecret: testClientSecret, RefreshToken: testRefreshToken, GrantedScope: test.scope,
			})
			_, err := refreshDriverFor(t, fake).Refresh(context.Background(), sdkgo.CredentialRefreshState[outlookmail.Credentials]{Credentials: delegatedCredentials(), Now: time.Now()})
			if test.isReauthorize {
				require.True(t, sdkgo.IsReauthorizationRequired(err), "%v", err)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestDelegatedRefreshClassifiesTerminalAndTransientErrorsWithoutProviderText(t *testing.T) {
	fake := newGraphFake(t)
	driver := refreshDriverFor(t, fake)
	revoked := delegatedCredentials()
	revoked.RefreshToken = sdkgo.NewSecretString("revoked-refresh-token")
	_, err := driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[outlookmail.Credentials]{Credentials: revoked, Now: time.Now()})
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.NotContains(t, err.Error(), graphtest.SentinelText)

	for _, fault := range []graphtest.Fault{{Status: http.StatusServiceUnavailable}, {ShouldDropConnection: true}} {
		fault.Endpoint = graphtest.EndpointDelegatedToken
		fake.InjectFault(fault)
		_, err = driver.Refresh(context.Background(), sdkgo.CredentialRefreshState[outlookmail.Credentials]{Credentials: delegatedCredentials(), Now: time.Now()})
		require.Error(t, err)
		require.False(t, sdkgo.IsReauthorizationRequired(err), "an outage is retried, not a reason to reauthorize")
	}
}

func TestRevokedGrantStopsOperationsUntilAuthorizedAgain(t *testing.T) {
	fake := newGraphFake(t)
	expired := time.Now().Add(-time.Minute)
	revoked := delegatedCredentials()
	revoked.RefreshToken = sdkgo.NewSecretString("revoked-refresh-token")
	credentials := graphtest.NewCredentialHost(revoked, &expired)
	client := newHostedClient(t, fake, outlookmail.Config{}, credentials)
	result, err := sdkgo.RunQuery(newOutlookDexContext("revoked"), client.SearchMessages(), outlookConnection, outlookmail.SearchMessagesInput{})
	require.NoError(t, err)
	require.Equal(t, outlookmail.SearchMessagesBranchProviderRejected, result.Branch)
	require.Equal(t, "Microsoft authorization must be renewed: authorize the connection again or replace the client secret", result.Failure.Message)
	_, isReauthorizationRequired := credentials.Stored()
	require.True(t, isReauthorizationRequired)
	require.Zero(t, fake.RequestCount(graphtest.EndpointListFolderMessages))
}

func TestAppOnlyTokenUsesTheTenantEndpointAndTheGraphDefaultScope(t *testing.T) {
	fake := newGraphFake(t)
	credentials := outlookmail.Credentials{
		AuthMethodID: outlookmail.AppOnlyAuthMethodID, TenantID: testTenantID, ClientID: testClientID(), ClientSecret: sdkgo.NewSecretString(testClientSecret),
	}
	result, err := refreshDriverFor(t, fake).Refresh(context.Background(), sdkgo.CredentialRefreshState[outlookmail.Credentials]{Credentials: credentials, Now: time.Now()})
	require.NoError(t, err)
	require.NotEmpty(t, result.Credentials.AccessToken.Reveal())
	require.Empty(t, result.Credentials.RefreshToken.Reveal(), "client credentials never return a refresh token")
	request := fake.Requests(graphtest.EndpointAppOnlyToken)[0]
	require.Equal(t, "/"+testTenantID+"/oauth2/v2.0/token", request.Path)
	form, err := url.ParseQuery(string(request.Body))
	require.NoError(t, err)
	require.Equal(t, url.Values{
		"grant_type": {"client_credentials"}, "client_id": {testClientID()}, "client_secret": {testClientSecret},
		"scope": {"https://graph.microsoft.com/.default"},
	}, form)
}

func TestAppOnlyTenantMustNameOneTenant(t *testing.T) {
	fake := newGraphFake(t)
	for _, tenant := range []string{"", "common", "Organizations", "consumers", "contoso", "contoso.onmicrosoft.com/../common", "a b.example"} {
		credentials := outlookmail.Credentials{
			AuthMethodID: outlookmail.AppOnlyAuthMethodID, TenantID: tenant, ClientID: testClientID(), ClientSecret: sdkgo.NewSecretString(testClientSecret),
		}
		_, err := refreshDriverFor(t, fake).Refresh(context.Background(), sdkgo.CredentialRefreshState[outlookmail.Credentials]{Credentials: credentials, Now: time.Now()})
		require.True(t, sdkgo.IsReauthorizationRequired(err), tenant)
	}
	require.Zero(t, fake.RequestCount(graphtest.EndpointAppOnlyToken))
	for index, tenant := range []string{"6b0e6a3c-1d2e-4f5a-9b8c-7d6e5f4a3b2c", "contoso.example"} {
		credentials := outlookmail.Credentials{
			AuthMethodID: outlookmail.AppOnlyAuthMethodID, TenantID: tenant, ClientID: testClientID(), ClientSecret: sdkgo.NewSecretString(testClientSecret),
		}
		// The fake knows one tenant, so these fail at the endpoint; the point is that they reach it.
		_, err := refreshDriverFor(t, fake).Refresh(context.Background(), sdkgo.CredentialRefreshState[outlookmail.Credentials]{Credentials: credentials, Now: time.Now()})
		require.Error(t, err)
		require.Equal(t, index+1, fake.RequestCount(graphtest.EndpointAppOnlyToken), tenant)
		require.Equal(t, "/"+tenant+"/oauth2/v2.0/token", fake.Requests(graphtest.EndpointAppOnlyToken)[index].Path)
	}
}

func TestRefreshRequiredFiveMinutesBeforeExpiry(t *testing.T) {
	driver := outlookmail.NewCredentialRefreshDriver(nil)
	now := time.Now()
	soon, later := now.Add(4*time.Minute), now.Add(time.Hour)
	credentials := delegatedCredentials()
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[outlookmail.Credentials]{Credentials: credentials, ExpiresAt: &soon, Now: now}))
	require.False(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[outlookmail.Credentials]{Credentials: credentials, ExpiresAt: &later, Now: now}))
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[outlookmail.Credentials]{Credentials: credentials, Now: now}), "no recorded expiry refreshes")
	credentials.AccessToken = sdkgo.NewSecretString("")
	require.True(t, driver.RefreshRequired(sdkgo.CredentialRefreshState[outlookmail.Credentials]{Credentials: credentials, ExpiresAt: &later, Now: now}))
}
