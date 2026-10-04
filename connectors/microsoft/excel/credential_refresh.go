// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	microsoftTokenEndpoint = "https://" + loginHost + "/organizations/oauth2/v2.0/token"
	// graphResourcePrefix is the resource URI Microsoft may put before a Graph permission in a returned scope.
	graphResourcePrefix = "https://graph.microsoft.com/"
	// credentialRefreshTimeout leaves most of the 30-second Execute timeout for the operation's requests.
	credentialRefreshTimeout = 8 * time.Second
)

var (
	// graphPermissions are the delegated permissions every operation needs, in the manifest's short form.
	graphPermissions = []string{"Files.ReadWrite.All"}
	// refreshScope repeats the manifest scopes, as Microsoft's refresh request example sends them.
	refreshScope = "offline_access " + strings.Join(graphPermissions, " ")
	// microsoftTerminalTokenErrorCodes are the token endpoint errors that Microsoft documents as needing new consent or a fixed registration.
	microsoftTerminalTokenErrorCodes = []string{
		"invalid_grant", "invalid_client", "unauthorized_client", "interaction_required", "consent_required",
		"invalid_scope", "invalid_request", "unsupported_grant_type", "invalid_resource",
	}

	errCredentialRefreshUnavailable = errors.New("Microsoft access token refresh is temporarily unavailable")
)

// CredentialRefreshDriver refreshes delegated Microsoft OAuth credentials with
// the refresh token grant at
// https://login.microsoftonline.com/organizations/oauth2/v2.0/token. It
// performs the token exchange but leaves locking and atomic persistence to the
// credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the provider-specific refresh driver.
// A nil HTTP client uses an 8-second client; the caller retains ownership of a
// supplied client. The token exchange never follows a redirect.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: credentialRefreshTimeout}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether the access token is absent, has no recorded
// expiry, or expires within oauthtoken.RefreshSkew. Microsoft access tokens always expire.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh exchanges the refresh token for a new access token, presenting the
// client ID and secret in the form body and requesting offline_access and
// Files.ReadWrite.All again. It stores the refresh token Microsoft returns, or
// keeps the prior one when Microsoft omits it. A terminal grant or client
// error, missing refresh material, or a returned scope without
// Files.ReadWrite.All returns an error wrapped by
// sdkgo.NewReauthorizationRequiredError; every other failure is retryable.
// offline_access is never required in the returned scope, because Microsoft
// does not echo it.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Microsoft Excel credential refresh driver is not configured")
	}
	credentials := state.Credentials
	if credentials.AuthMethodID != OAuthAuthMethodID {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Microsoft Excel authorization method is not supported"))
	}
	ctx, cancel := context.WithTimeout(ctx, credentialRefreshTimeout)
	defer cancel()
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:                "Microsoft",
		URL:                         microsoftTokenEndpoint,
		HTTPClient:                  driver.httpClient,
		TerminalErrorCodes:          microsoftTerminalTokenErrorCodes,
		AdditionalRequestParameters: map[string]string{"scope": refreshScope},
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID: credentials.OAuthClientID, Secret: credentials.OAuthClientSecret, AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, credentials.RefreshToken)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return sdkgo.CredentialRefreshResult[Credentials]{}, err
		}
		return sdkgo.CredentialRefreshResult[Credentials]{}, fmt.Errorf("%w: %w", errCredentialRefreshUnavailable, err)
	}
	if !hasGraphPermissions(token.Scope, graphPermissions) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(
			fmt.Errorf("Microsoft granted a token without the %s permission", strings.Join(graphPermissions, ", ")))
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}

// validateResolvedCredentials checks only what a request needs; renewal material is not required.
func validateResolvedCredentials(credentials Credentials) error {
	if credentials.AuthMethodID != OAuthAuthMethodID {
		return errors.New("Microsoft Excel auth_method is invalid")
	}
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("Microsoft Excel access token is missing or is not printable ASCII without spaces")
	}
	return nil
}

// hasGraphPermissions accepts an omitted scope and short or resource-qualified permission names, compared without case.
func hasGraphPermissions(returnedScope string, requiredPermissions []string) bool {
	if strings.TrimSpace(returnedScope) == "" {
		return true
	}
	granted := map[string]bool{}
	for _, scope := range strings.FieldsFunc(returnedScope, func(character rune) bool { return character == ' ' || character == ',' }) {
		lowercase := strings.ToLower(scope)
		granted[strings.TrimPrefix(lowercase, graphResourcePrefix)] = true
	}
	for _, permission := range requiredPermissions {
		if !granted[strings.ToLower(permission)] {
			return false
		}
	}
	return true
}
