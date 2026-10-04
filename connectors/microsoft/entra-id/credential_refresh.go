// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package entraid

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const (
	// EntraAppOnlyAuthMethodID identifies app-only access with the client credentials grant.
	EntraAppOnlyAuthMethodID = "entra-app-only"
	// MicrosoftOAuthAuthMethodID identifies interactive Microsoft OAuth consent by an administrator.
	MicrosoftOAuthAuthMethodID = "microsoft-oauth"

	// organizationsTokenEndpoint is the static endpoint the manifest declares; it serves any work or school tenant.
	organizationsTokenEndpoint = "https://" + microsoftLoginHost + "/organizations/oauth2/v2.0/token"
	// graphApplicationScope asks for every Microsoft Graph application permission an administrator granted the app.
	graphApplicationScope = "https://graph.microsoft.com/.default"
	graphScopePrefix      = "https://graph.microsoft.com/"
)

// delegatedGraphScopes are the manifest's Graph permissions without offline_access, which Microsoft does not echo.
var delegatedGraphScopes = []string{
	"User.Read.All", "User.Create", "User.EnableDisableAccount.All", "User.RevokeSessions.All", "GroupMember.ReadWrite.All",
}

// microsoftTerminalTokenErrorCodes are the documented token endpoint errors that no retry can recover.
var microsoftTerminalTokenErrorCodes = []string{
	"invalid_request", "invalid_grant", "unauthorized_client", "invalid_client", "unsupported_grant_type",
	"invalid_resource", "interaction_required", "consent_required", "invalid_scope",
}

// tenantIDPattern accepts a directory ID; tenantDomainPattern accepts a verified domain such as contoso.onmicrosoft.com.
var (
	tenantIDPattern     = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
	tenantDomainPattern = regexp.MustCompile(`^(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?)(?:\.(?i:[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?))+$`)
)

// CredentialRefreshDriver obtains Microsoft Graph access tokens for both authorization methods: the client
// credentials grant against the tenant's token endpoint for app-only access, and the refresh-token grant
// against the organizations endpoint for Microsoft OAuth. It performs token endpoint calls but leaves locking
// and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the provider-specific refresh driver.
// A nil HTTP client uses a 25-second client; the caller retains ownership of a supplied client.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether the access token is absent, has no recorded
// expiry, or expires within oauthtoken.RefreshSkew. Microsoft access tokens always expire.
func (driver *CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh obtains a replacement access token for the selected authorization method. An invalid
// tenant ID, missing client or refresh material, a terminal Microsoft token error, and a delegated
// token without the five Graph permissions return an error wrapped by
// sdkgo.NewReauthorizationRequiredError; a Microsoft 5xx or transport failure is retryable.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Microsoft Entra ID credential refresh driver is not configured")
	}
	switch state.Credentials.AuthMethodID {
	case EntraAppOnlyAuthMethodID:
		return driver.refreshAppOnly(ctx, state.Credentials)
	case MicrosoftOAuthAuthMethodID:
		return driver.refreshMicrosoftOAuth(ctx, state.Credentials)
	default:
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Microsoft Entra ID authorization method is not supported"))
	}
}

func (driver *CredentialRefreshDriver) refreshAppOnly(ctx context.Context, credentials Credentials) (sdkgo.CredentialRefreshResult[Credentials], error) {
	tokenEndpointURL, err := tenantTokenEndpoint(credentials.TenantID)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(err)
	}
	token, err := driver.microsoftTokenEndpoint(tokenEndpointURL).ExchangeClientCredentials(ctx, oauthtoken.ClientCredentials{
		ID:                   strings.TrimSpace(credentials.ClientID),
		Secret:               credentials.ClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, graphApplicationScope)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	credentials.AccessToken = token.AccessToken
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}

func (driver *CredentialRefreshDriver) refreshMicrosoftOAuth(ctx context.Context, credentials Credentials) (sdkgo.CredentialRefreshResult[Credentials], error) {
	endpoint := driver.microsoftTokenEndpoint(organizationsTokenEndpoint)
	// The scope narrows the new access token to Graph; Microsoft documents it as optional on refresh.
	endpoint.AdditionalRequestParameters = map[string]string{"scope": "offline_access " + strings.Join(delegatedGraphScopes, " ")}
	token, err := endpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   strings.TrimSpace(credentials.ClientID),
		Secret:               credentials.ClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, credentials.RefreshToken)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if err := validateReturnedDelegatedScopes(token.Scope); err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(err)
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}

func (driver *CredentialRefreshDriver) microsoftTokenEndpoint(tokenEndpointURL string) oauthtoken.TokenEndpoint {
	return oauthtoken.TokenEndpoint{
		ProviderName:       "Microsoft",
		URL:                tokenEndpointURL,
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: microsoftTerminalTokenErrorCodes,
	}
}

// tenantTokenEndpoint validates the tenant before it becomes a path; client credentials need one tenant, not an alias.
func tenantTokenEndpoint(rawTenantID string) (string, error) {
	tenantID := strings.TrimSpace(rawTenantID)
	switch strings.ToLower(tenantID) {
	case "common", "organizations", "consumers":
		return "", errors.New("Microsoft Entra tenant must be a directory ID or a verified domain, not a multi-tenant alias")
	}
	if !tenantIDPattern.MatchString(tenantID) && (len(tenantID) > 253 || !tenantDomainPattern.MatchString(tenantID)) {
		return "", errors.New("Microsoft Entra tenant must be a directory ID such as 00000000-0000-0000-0000-000000000000 or a verified domain such as contoso.onmicrosoft.com")
	}
	return "https://" + microsoftLoginHost + "/" + strings.ToLower(tenantID) + "/oauth2/v2.0/token", nil
}

// validateReturnedDelegatedScopes accepts an omitted scope value and either Microsoft scope form,
// such as User.Read.All or https://graph.microsoft.com/User.Read.All, compared without case.
func validateReturnedDelegatedScopes(scope string) error {
	if strings.TrimSpace(scope) == "" {
		return nil
	}
	granted := map[string]bool{}
	for _, value := range strings.Fields(scope) {
		lowered := strings.ToLower(value)
		granted[strings.TrimPrefix(lowered, strings.ToLower(graphScopePrefix))] = true
	}
	missing := []string{}
	for _, required := range delegatedGraphScopes {
		if !granted[strings.ToLower(required)] {
			missing = append(missing, required)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("Microsoft credential lacks the required Graph permissions: %s", strings.Join(missing, ", "))
	}
	return nil
}
