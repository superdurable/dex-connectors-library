// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	outlookcalendar "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar"
	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/localconfig"
)

// The fake client secret is split so it never resembles a real Entra secret.
var fakeClientSecret = "fake-client" + "-secret"

func delegatedRefreshState() sdkgo.CredentialRefreshState[outlookcalendar.Credentials] {
	return sdkgo.CredentialRefreshState[outlookcalendar.Credentials]{Credentials: outlookcalendar.Credentials{
		AuthMethodID: outlookcalendar.MicrosoftOAuthAuthMethodID, ClientID: "client-id", ClientSecret: sdkgo.NewSecretString(fakeClientSecret),
		AccessToken: sdkgo.NewSecretString("old-access"), RefreshToken: sdkgo.NewSecretString("old-refresh"),
	}, Now: time.Now()}
}

func appOnlyRefreshState() sdkgo.CredentialRefreshState[outlookcalendar.Credentials] {
	return sdkgo.CredentialRefreshState[outlookcalendar.Credentials]{Credentials: outlookcalendar.Credentials{
		AuthMethodID: outlookcalendar.AppOnlyAuthMethodID, ClientID: "client-id", ClientSecret: sdkgo.NewSecretString(fakeClientSecret),
	}, Now: time.Now()}
}

func newRoutedDriver(t *testing.T, providerURL string, tenantID string) *outlookcalendar.CredentialRefreshDriver {
	t.Helper()
	target, err := url.Parse(providerURL)
	require.NoError(t, err)
	return outlookcalendar.NewCredentialRefreshDriver(&http.Client{Transport: hostRewritingTransport{target: target}}, tenantID)
}

// hostRewritingTransport sends Microsoft identity requests to the local fake.
type hostRewritingTransport struct{ target *url.URL }

func (transport hostRewritingTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	rewritten := request.Clone(request.Context())
	rewritten.URL.Scheme, rewritten.URL.Host, rewritten.Host = transport.target.Scheme, transport.target.Host, transport.target.Host
	return http.DefaultTransport.RoundTrip(rewritten)
}

func TestDelegatedRefreshRotatesTheRefreshTokenAtTheOrganizationsEndpoint(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"token_type":"Bearer","expires_in":3599,"scope":"Calendars.ReadWrite User.Read","access_token":"new-access","refresh_token":"new-refresh"}`)
	})
	driver := newRoutedDriver(t, provider.URL, "")

	result, err := driver.Refresh(context.Background(), delegatedRefreshState())
	require.NoError(t, err)
	require.Equal(t, "new-access", result.Credentials.AccessToken.Reveal())
	require.Equal(t, "new-refresh", result.Credentials.RefreshToken.Reveal(), "Microsoft rotates refresh tokens; the new one is kept")
	require.WithinDuration(t, time.Now().Add(3599*time.Second), result.ExpiresAt, 5*time.Second)
	request := provider.request(t, 0)
	require.Equal(t, "/organizations/oauth2/v2.0/token", request.path)
	form, err := url.ParseQuery(string(request.body))
	require.NoError(t, err)
	require.Equal(t, url.Values{
		"grant_type": {"refresh_token"}, "refresh_token": {"old-refresh"}, "client_id": {"client-id"}, "client_secret": {fakeClientSecret},
	}, form, "scope is optional on a refresh, so the original grant is kept")
}

func TestDelegatedRefreshKeepsThePriorRefreshTokenWhenNoneIsReturned(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"token_type":"Bearer","expires_in":3599,"access_token":"new-access"}`)
	})
	result, err := newRoutedDriver(t, provider.URL, "").Refresh(context.Background(), delegatedRefreshState())
	require.NoError(t, err)
	require.Equal(t, "old-refresh", result.Credentials.RefreshToken.Reveal())
}

func TestDelegatedRefreshRequiresReauthorizationForLostConsent(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "invalid_grant", status: http.StatusBadRequest, body: `{"error":"invalid_grant","error_description":"SENTINEL AADSTS70000"}`},
		{name: "interaction_required", status: http.StatusBadRequest, body: `{"error":"interaction_required"}`},
		{name: "calendar permission removed", status: http.StatusOK, body: `{"token_type":"Bearer","expires_in":3599,"scope":"https://graph.microsoft.com/User.Read","access_token":"new-access"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
				writeJSON(t, response, test.status, test.body)
			})
			_, err := newRoutedDriver(t, provider.URL, "").Refresh(context.Background(), delegatedRefreshState())
			require.True(t, sdkgo.IsReauthorizationRequired(err), "%v", err)
			require.NotContains(t, err.Error(), "SENTINEL")
		})
	}
}

func TestDelegatedRefreshServerErrorIsRetryable(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusServiceUnavailable, `{"error":"temporarily_unavailable"}`)
	})
	_, err := newRoutedDriver(t, provider.URL, "").Refresh(context.Background(), delegatedRefreshState())
	require.Error(t, err)
	require.False(t, sdkgo.IsReauthorizationRequired(err))
}

func TestAppOnlyTokenUsesClientCredentialsAtTheConfiguredTenant(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusOK, `{"token_type":"Bearer","expires_in":3599,"ext_expires_in":3599,"access_token":"app-access"}`)
	})
	result, err := newRoutedDriver(t, provider.URL, "contoso.onmicrosoft.com").Refresh(context.Background(), appOnlyRefreshState())
	require.NoError(t, err)
	require.Equal(t, "app-access", result.Credentials.AccessToken.Reveal())
	request := provider.request(t, 0)
	require.Equal(t, "/contoso.onmicrosoft.com/oauth2/v2.0/token", request.path)
	form, err := url.ParseQuery(string(request.body))
	require.NoError(t, err)
	require.Equal(t, url.Values{
		"grant_type": {"client_credentials"}, "scope": {"https://graph.microsoft.com/.default"},
		"client_id": {"client-id"}, "client_secret": {fakeClientSecret},
	}, form)
}

func TestAppOnlyTokenRequiresAValidTenantAndAcceptedClient(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, _ *http.Request, _ int) {
		writeJSON(t, response, http.StatusUnauthorized, `{"error":"invalid_client","error_codes":[7000215]}`)
	})
	_, err := newRoutedDriver(t, provider.URL, "").Refresh(context.Background(), appOnlyRefreshState())
	require.True(t, sdkgo.IsReauthorizationRequired(err))
	require.Zero(t, provider.requestCount(), "a blank tenant never reaches Microsoft")

	_, err = newRoutedDriver(t, provider.URL, "72f988bf-86f1-41af-91ab-2d7cd011db47").Refresh(context.Background(), appOnlyRefreshState())
	require.True(t, sdkgo.IsReauthorizationRequired(err), "a rejected client secret must be replaced")
	require.Equal(t, 1, provider.requestCount())
}

func TestRefreshRequiredForAMissingOrExpiringToken(t *testing.T) {
	driver := outlookcalendar.NewCredentialRefreshDriver(nil, "")
	now := time.Now()
	later, soon := now.Add(time.Hour), now.Add(time.Minute)
	state := delegatedRefreshState()
	state.Now, state.ExpiresAt = now, &later
	require.False(t, driver.RefreshRequired(state))
	state.ExpiresAt = &soon
	require.True(t, driver.RefreshRequired(state))
	state.ExpiresAt = nil
	require.True(t, driver.RefreshRequired(state), "a token without a recorded expiry is replaced")
	require.True(t, driver.RefreshRequired(appOnlyRefreshState()), "an app-only connection starts without a token")
}

func TestDecodeResolvedCredentialsJSONAcceptsOnlyAnAccessToken(t *testing.T) {
	credentials, err := outlookcalendar.DecodeResolvedCredentialsJSON(json.RawMessage(`{"auth_method":"app-only","access_token":"resolved"}`))
	require.NoError(t, err)
	require.Equal(t, "resolved", credentials.AccessToken.Reveal())
	for _, contents := range []string{
		`{"auth_method":"microsoft-oauth","access_token":"a","refresh_token":"b"}`, `{"auth_method":"app-only","access_token":"has space"}`,
		`{"auth_method":"other","access_token":"a"}`, `{}`, `[]`,
	} {
		_, err := outlookcalendar.DecodeResolvedCredentialsJSON(json.RawMessage(contents))
		require.Error(t, err, contents)
		require.NotContains(t, err.Error(), "has space")
	}
}

// TestLocalAppOnlyConnectionRequestsAndStoresATokenBeforeItsFirstCall runs the generated
// NewLocalConnection against the file Dex Web writes for an app-only connection.
func TestLocalAppOnlyConnectionRequestsAndStoresATokenBeforeItsFirstCall(t *testing.T) {
	provider := newRecordingProvider(t, func(response http.ResponseWriter, request *http.Request, _ int) {
		switch request.URL.Path {
		case "/72f988bf-86f1-41af-91ab-2d7cd011db47/oauth2/v2.0/token":
			writeJSON(t, response, http.StatusOK, `{"token_type":"Bearer","expires_in":3599,"access_token":"stored-app-token"}`)
		default:
			require.Equal(t, "Bearer stored-app-token", request.Header.Get("Authorization"))
			writeJSON(t, response, http.StatusOK, timedEventJSON("event-1", "Planning"))
		}
	})
	configPath := writeAppOnlyConnectionFile(t)
	store, err := localconfig.LoadFile(configPath)
	require.NoError(t, err)
	connection, err := outlookcalendar.NewLocalConnection(store, calendarConnection.Name, outlookcalendar.WithLocalProviderURL(provider.URL))
	require.NoError(t, err)

	result, err := sdkgo.RunQuery(newCalendarDexContext("local-app-only"), outlookcalendar.ClientOfConnection(connection).GetEvent(), calendarConnection,
		outlookcalendar.GetEventInput{EventID: "event-1"})
	require.NoError(t, err)
	require.Equal(t, outlookcalendar.GetEventBranchFound, result.Branch, "%+v", result.Failure)
	require.Equal(t, "/v1.0/users/scheduling@contoso.com/events/event-1", provider.request(t, 1).path)

	contents, err := os.ReadFile(configPath)
	require.NoError(t, err)
	var file struct {
		Connections []struct {
			Credentials         map[string]any `json:"credentials"`
			CredentialExpiresAt time.Time      `json:"credentialExpiresAt"`
			AuthMethodID        string         `json:"authMethodId"`
		} `json:"connections"`
	}
	require.NoError(t, json.Unmarshal(contents, &file))
	require.Equal(t, "stored-app-token", file.Connections[0].Credentials["access_token"], "the Studio picker reads the stored token")
	require.True(t, file.Connections[0].CredentialExpiresAt.After(time.Now()))
	require.Equal(t, "app-only", file.Connections[0].AuthMethodID, "Dex Web's record members survive the refresh")
}

func writeAppOnlyConnectionFile(t *testing.T) string {
	t.Helper()
	record := map[string]any{
		"connectorId": outlookcalendar.ConnectorID, "modulePath": "github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-calendar",
		"moduleVersion": "v0.1.0", "provider": "microsoft", "connectionName": calendarConnection.Name, "authMethodId": "app-only",
		"configuration": map[string]any{"tenantId": "72f988bf-86f1-41af-91ab-2d7cd011db47", "mailbox": "scheduling@contoso.com"},
		"credentials":   map[string]any{"auth_method": "app-only", "client_id": "client-id", "client_secret": fakeClientSecret},
	}
	contents, err := json.Marshal(map[string]any{"schemaVersion": localconfig.SchemaVersion, "connections": []any{record}})
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "connections.json")
	require.NoError(t, os.WriteFile(path, contents, 0o600))
	return path
}
