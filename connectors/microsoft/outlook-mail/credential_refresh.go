// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package outlookmail

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
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	delegatedTokenEndpoint = "https://" + loginHost + "/organizations/oauth2/v2.0/token"
	// graphDefaultScope asks for every application permission or Exchange role granted to the app.
	graphDefaultScope = "https://graph.microsoft.com/.default"
	graphScopePrefix  = "https://graph.microsoft.com/"
	// credentialRefreshTimeout leaves most of the 30-second Execute timeout for the operation request.
	credentialRefreshTimeout = 8 * time.Second
)

var (
	// delegatedMailScopes must match the manifest; offline_access is requested but never echoed back.
	delegatedMailScopes = []string{"Mail.ReadWrite", "Mail.Send"}
	// delegatedRefreshScope repeats the consented scopes, so the refreshed token is for Microsoft Graph.
	delegatedRefreshScope = "offline_access " + strings.Join(delegatedMailScopes, " ")
	// terminalTokenErrorCodes are the identity platform errors that no retry can recover from.
	terminalTokenErrorCodes = []string{
		"invalid_request", "invalid_grant", "unauthorized_client", "invalid_client", "unsupported_grant_type",
		"invalid_resource", "interaction_required", "consent_required", "invalid_scope",
	}
	// tenantDomainPattern is a DNS name such as contoso.onmicrosoft.com.
	tenantDomainPattern = regexp.MustCompile(`^(?:[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?\.)+[A-Za-z]{2,63}$`)

	errCredentialRefreshUnavailable = errors.New("Microsoft access token request is temporarily unavailable")
)

// CredentialRefreshDriver obtains Microsoft Graph access tokens. A delegated connection exchanges its
// rotating refresh token at the shared organizations endpoint; an app-only connection repeats the client
// credentials grant at its tenant's endpoint. Both send the client secret in the form body. The driver
// calls Microsoft but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the provider-specific refresh driver. A nil HTTP client uses an
// 8-second client; the caller retains ownership of a supplied client. An exchange never follows a redirect.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: credentialRefreshTimeout}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether the access token is absent, has no recorded expiry, or expires within
// five minutes. Microsoft access tokens last about an hour for both methods.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh obtains a replacement access token for the selected method. A delegated refresh keeps the
// refresh token Microsoft rotates, or the prior one if Microsoft omits it, and requires Mail.ReadWrite and
// Mail.Send in a returned scope; offline_access is never required, because Microsoft does not echo it. A
// rejected client, grant, consent, or scope, or an invalid tenant ID, returns an error wrapped by
// sdkgo.NewReauthorizationRequiredError; every other failure is retryable.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Outlook Mail credential refresh driver is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, credentialRefreshTimeout)
	defer cancel()
	credentials := state.Credentials
	client := oauthtoken.ClientCredentials{
		ID: credentials.ClientID, Secret: credentials.ClientSecret, AuthenticationMethod: oauthtoken.ClientSecretPost,
	}
	var token oauthtoken.TokenResponse
	var err error
	switch credentials.AuthMethodID {
	case MicrosoftOAuthAuthMethodID:
		endpoint := driver.tokenEndpoint(delegatedTokenEndpoint)
		endpoint.AdditionalRequestParameters = map[string]string{"scope": delegatedRefreshScope}
		token, err = endpoint.ExchangeRefreshToken(ctx, client, credentials.RefreshToken)
	case AppOnlyAuthMethodID:
		tenant := strings.TrimSpace(credentials.TenantID)
		if tenantErr := validateTenantID(tenant); tenantErr != nil {
			return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(tenantErr)
		}
		endpoint := driver.tokenEndpoint("https://" + loginHost + "/" + tenant + "/oauth2/v2.0/token")
		token, err = endpoint.ExchangeClientCredentials(ctx, client, graphDefaultScope)
	default:
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Outlook Mail authorization method is not supported"))
	}
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return sdkgo.CredentialRefreshResult[Credentials]{}, err
		}
		return sdkgo.CredentialRefreshResult[Credentials]{}, fmt.Errorf("%w: %w", errCredentialRefreshUnavailable, err)
	}
	if credentials.AuthMethodID == MicrosoftOAuthAuthMethodID {
		if token.Scope != "" && !hasGrantedGraphScopes(token.Scope, delegatedMailScopes) {
			return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(
				errors.New("Microsoft granted a token without Mail.ReadWrite or Mail.Send"))
		}
		credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	}
	credentials.AccessToken = token.AccessToken
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}

func (driver *CredentialRefreshDriver) tokenEndpoint(endpointURL string) oauthtoken.TokenEndpoint {
	return oauthtoken.TokenEndpoint{
		ProviderName: "Microsoft", URL: endpointURL, HTTPClient: driver.httpClient, TerminalErrorCodes: terminalTokenErrorCodes,
	}
}

// validateResolvedCredentials checks only what a request needs; renewal material is not required.
func validateResolvedCredentials(credentials Credentials) error {
	switch credentials.AuthMethodID {
	case MicrosoftOAuthAuthMethodID, AppOnlyAuthMethodID:
	default:
		return errors.New("Outlook Mail auth_method is invalid")
	}
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("Outlook Mail access token is missing or is not printable ASCII without spaces")
	}
	return nil
}

// validateTenantID accepts a tenant GUID or verified domain; the shared endpoints cannot issue app-only tokens.
func validateTenantID(tenant string) error {
	switch strings.ToLower(tenant) {
	case "":
		return errors.New("credential tenant_id is required")
	case "common", "organizations", "consumers":
		return errors.New("credential tenant_id must name one tenant, not common, organizations, or consumers")
	}
	if guidPattern.MatchString(tenant) || (len(tenant) <= 253 && tenantDomainPattern.MatchString(tenant)) {
		return nil
	}
	return errors.New("credential tenant_id must be a tenant GUID or a domain such as contoso.onmicrosoft.com")
}

// hasGrantedGraphScopes compares Graph permission names without case, accepting the resource-prefixed form.
func hasGrantedGraphScopes(returnedScope string, requiredScopes []string) bool {
	granted := map[string]bool{}
	for _, scope := range strings.Fields(returnedScope) {
		if len(scope) > len(graphScopePrefix) && strings.EqualFold(scope[:len(graphScopePrefix)], graphScopePrefix) {
			scope = scope[len(graphScopePrefix):]
		}
		granted[strings.ToLower(scope)] = true
	}
	for _, scope := range requiredScopes {
		if !granted[strings.ToLower(scope)] {
			return false
		}
	}
	return true
}
