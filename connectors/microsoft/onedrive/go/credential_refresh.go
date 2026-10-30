// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package onedrive

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const (
	// MicrosoftOAuthAuthMethodID identifies delegated Microsoft work or school OAuth authorization.
	MicrosoftOAuthAuthMethodID = "microsoft-oauth"
	// MicrosoftAppOnlyAuthMethodID identifies app-only authorization with the client credentials grant.
	MicrosoftAppOnlyAuthMethodID = "microsoft-app-only"

	// microsoftOrganizationsTokenEndpoint matches the manifest's static multi-tenant token endpoint.
	microsoftOrganizationsTokenEndpoint = "https://login.microsoftonline.com/organizations/oauth2/v2.0/token"
	microsoftLoginOrigin                = "https://login.microsoftonline.com/"
	microsoftTokenEndpointPath          = "/oauth2/v2.0/token"
	// microsoftGraphDefaultScope requests every application permission the tenant granted the app.
	microsoftGraphDefaultScope = "https://graph.microsoft.com/.default"
	microsoftGraphScopePrefix  = "https://graph.microsoft.com/"
	// maxTenantDomainBytes is the DNS name limit for a verified tenant domain.
	maxTenantDomainBytes = 253
)

// delegatedScopes are the manifest's delegated scopes, sent again on refresh.
var delegatedScopes = []string{"Files.ReadWrite.All", "Sites.Read.All", "offline_access"}

// requiredDelegatedScopes are the scopes every operation needs; Sites.Read.All only serves the site picker.
var requiredDelegatedScopes = []string{"Files.ReadWrite.All"}

// microsoftTerminalTokenErrorCodes are Microsoft identity platform token errors that no retry can recover.
var microsoftTerminalTokenErrorCodes = []string{
	"invalid_grant", "invalid_client", "unauthorized_client", "invalid_request", "invalid_scope",
	"interaction_required", "consent_required",
}

var (
	tenantGUIDPattern   = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
	tenantDomainPattern = regexp.MustCompile(`^(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$`)
)

// CredentialRefreshDriver refreshes delegated Microsoft OAuth access tokens and
// requests app-only access tokens with the client credentials grant. It
// performs token endpoint calls but leaves locking and atomic persistence to
// the credential provider.
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
// expiry, or expires within oauthtoken.RefreshSkew. Microsoft access tokens
// always expire, so a token without a recorded expiry is replaced.
func (driver *CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh obtains a replacement access token for the selected method. The
// delegated method uses the refresh-token grant at the organizations token
// endpoint and keeps Microsoft's rotated refresh token. The app-only method
// requests https://graph.microsoft.com/.default from the configured tenant's
// token endpoint. Terminal Microsoft token errors, missing material, an
// invalid tenant ID, and a delegated grant without Files.ReadWrite.All return
// an error wrapped by sdkgo.NewReauthorizationRequiredError.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Microsoft credential refresh driver is not configured")
	}
	switch state.Credentials.AuthMethodID {
	case "", MicrosoftOAuthAuthMethodID:
		return driver.refreshDelegatedToken(ctx, state.Credentials)
	case MicrosoftAppOnlyAuthMethodID:
		return driver.requestAppOnlyToken(ctx, state.Credentials)
	default:
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Microsoft OneDrive authorization method is not supported"))
	}
}

func (driver *CredentialRefreshDriver) refreshDelegatedToken(
	ctx context.Context,
	credentials Credentials,
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	endpoint := driver.microsoftTokenEndpoint(microsoftOrganizationsTokenEndpoint)
	endpoint.AdditionalRequestParameters = map[string]string{"scope": strings.Join(delegatedScopes, " ")}
	token, err := endpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.OAuthClientID,
		Secret:               credentials.OAuthClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, credentials.RefreshToken)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if !hasRequiredDelegatedScopes(token.Scope) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(
			errors.New("Microsoft credential lacks the required Files.ReadWrite.All delegated permission"))
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
	tenantID := strings.TrimSpace(credentials.TenantID)
	if !isValidTenantID(tenantID) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(
			errors.New("Microsoft tenant ID must be a directory GUID or a verified domain"))
	}
	endpoint := driver.microsoftTokenEndpoint(microsoftLoginOrigin + strings.ToLower(tenantID) + microsoftTokenEndpointPath)
	token, err := endpoint.ExchangeClientCredentials(ctx, oauthtoken.ClientCredentials{
		ID:                   strings.TrimSpace(credentials.AppClientID),
		Secret:               credentials.AppClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, microsoftGraphDefaultScope)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	credentials.AccessToken = token.AccessToken
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

// isValidTenantID accepts a directory GUID or a DNS domain; common, organizations, and consumers have no dot.
func isValidTenantID(tenantID string) bool {
	return tenantGUIDPattern.MatchString(tenantID) ||
		(len(tenantID) <= maxTenantDomainBytes && tenantDomainPattern.MatchString(tenantID))
}

// hasRequiredDelegatedScopes accepts an omitted scope value, short scope names,
// and https://graph.microsoft.com/ prefixed names, compared without case as Graph does.
func hasRequiredDelegatedScopes(returnedScope string) bool {
	if strings.TrimSpace(returnedScope) == "" {
		return true
	}
	granted := map[string]bool{}
	for _, scope := range strings.Fields(returnedScope) {
		granted[strings.ToLower(strings.TrimPrefix(scope, microsoftGraphScopePrefix))] = true
	}
	for _, scope := range requiredDelegatedScopes {
		if !granted[strings.ToLower(scope)] {
			return false
		}
	}
	return true
}
