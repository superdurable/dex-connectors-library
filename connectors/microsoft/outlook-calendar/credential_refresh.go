// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookcalendar

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// MicrosoftOAuthAuthMethodID identifies delegated Microsoft OAuth authorization of one work or school user.
	MicrosoftOAuthAuthMethodID = "microsoft-oauth"
	// AppOnlyAuthMethodID identifies an app-only client credentials connection that targets one configured mailbox.
	AppOnlyAuthMethodID = "app-only"

	identityBaseURL = "https://" + identityHost
	// delegatedTokenEndpoint matches the manifest's static multi-tenant OAuth endpoints.
	delegatedTokenEndpoint = identityBaseURL + "/organizations/oauth2/v2.0/token"
	// graphDefaultScope asks for every application permission an administrator granted the app.
	graphDefaultScope   = "https://graph.microsoft.com/.default"
	graphScopeURIPrefix = "https://graph.microsoft.com/"
)

// delegatedCalendarScope is the Graph permission every operation and the calendar picker need.
const delegatedCalendarScope = "Calendars.ReadWrite"

// Microsoft identity platform error codes that no retry can recover.
var (
	delegatedTerminalRefreshErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client", "interaction_required", "consent_required"}
	appOnlyTerminalTokenErrorCodes     = []string{"invalid_client", "unauthorized_client", "invalid_request", "invalid_scope"}
)

var (
	tenantGUIDPattern   = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
	tenantDomainPattern = regexp.MustCompile(`^(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$`)
)

// CredentialRefreshDriver obtains Microsoft Graph access tokens for both authorization methods.
// Delegated connections exchange their rotating refresh token at the multi-tenant organizations
// endpoint; app-only connections repeat the client credentials grant at their own tenant's endpoint.
// It performs the provider exchange but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	tenantID   string
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the driver. tenantID is the app-only connection's directory,
// a GUID or verified domain such as contoso.onmicrosoft.com; delegated connections ignore it. A nil
// HTTP client uses a 25-second client; the caller keeps ownership of a supplied client.
func NewCredentialRefreshDriver(httpClient *http.Client, tenantID string) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, tenantID: strings.TrimSpace(tenantID), now: time.Now}
}

// RefreshRequired reports whether the access token is absent, has no recorded expiry, or
// expires within five minutes.
func (driver *CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh obtains a replacement access token for the selected authorization method.
// Terminal Microsoft responses, an invalid tenant ID, and a delegated grant without
// Calendars.ReadWrite require reauthorization; every other failure is retryable.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Outlook Calendar credential refresh driver is not configured")
	}
	switch state.Credentials.AuthMethodID {
	case MicrosoftOAuthAuthMethodID:
		return driver.refreshDelegatedToken(ctx, state.Credentials)
	case AppOnlyAuthMethodID:
		return driver.requestAppOnlyToken(ctx, state.Credentials)
	default:
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Outlook Calendar authorization method is not supported")
	}
}

func (driver *CredentialRefreshDriver) refreshDelegatedToken(
	ctx context.Context,
	credentials Credentials,
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	// Microsoft documents scope as optional on a refresh; omitting it keeps the original grant.
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:       "Microsoft",
		URL:                delegatedTokenEndpoint,
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: delegatedTerminalRefreshErrorCodes,
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.ClientID,
		Secret:               credentials.ClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, credentials.RefreshToken)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if !hasDelegatedCalendarScope(token.Scope) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(
			errors.New("Microsoft credential lacks Calendars.ReadWrite; reconnect and accept every requested permission"))
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}

func (driver *CredentialRefreshDriver) requestAppOnlyToken(
	ctx context.Context,
	credentials Credentials,
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if err := ValidateTenantID(driver.tenantID); err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(err)
	}
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:       "Microsoft",
		URL:                identityBaseURL + "/" + driver.tenantID + "/oauth2/v2.0/token",
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: appOnlyTerminalTokenErrorCodes,
	}
	token, err := tokenEndpoint.ExchangeClientCredentials(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.ClientID,
		Secret:               credentials.ClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, graphDefaultScope)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	credentials.AccessToken = token.AccessToken
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}

// ValidateTenantID reports whether tenantID names one Microsoft Entra tenant: a directory
// (tenant) ID GUID or a verified domain such as contoso.onmicrosoft.com. The multi-tenant
// aliases common, organizations, and consumers are rejected because the client credentials
// grant needs one tenant. The error never repeats the value.
func ValidateTenantID(tenantID string) error {
	switch {
	case tenantID == "":
		return errors.New("tenantId is required for an app-only connection")
	case tenantGUIDPattern.MatchString(tenantID):
		return nil
	case len(tenantID) <= 253 && tenantDomainPattern.MatchString(tenantID):
		return nil
	default:
		return errors.New("tenantId must be a directory (tenant) ID GUID or a verified domain such as contoso.onmicrosoft.com")
	}
}

// hasDelegatedCalendarScope accepts an omitted scope or Calendars.ReadWrite in any case, short or URI form.
func hasDelegatedCalendarScope(scope string) bool {
	if strings.TrimSpace(scope) == "" {
		return true
	}
	for _, granted := range strings.Fields(scope) {
		if strings.EqualFold(strings.TrimPrefix(strings.ToLower(granted), graphScopeURIPrefix), delegatedCalendarScope) {
			return true
		}
	}
	return false
}

func validateResolvedCredentials(credentials Credentials) error {
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("Outlook Calendar access token is missing or cannot travel in an HTTP header")
	}
	switch credentials.AuthMethodID {
	case MicrosoftOAuthAuthMethodID, AppOnlyAuthMethodID:
		return nil
	default:
		return errors.New("Outlook Calendar authorization method is invalid")
	}
}
