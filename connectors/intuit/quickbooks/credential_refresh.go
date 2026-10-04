// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
	intuitTokenEndpoint = "https://" + tokenHost + "/oauth2/v1/tokens/bearer"
	// intuitIssuer is the iss claim of every Intuit ID token.
	intuitIssuer = "https://oauth.platform.intuit.com/op/v1"
	// credentialRefreshTimeout leaves most of the 30-second Execute timeout for the operation request.
	credentialRefreshTimeout = 8 * time.Second
	// maximumIDTokenBytes bounds the ID token the connector decodes.
	maximumIDTokenBytes = 16 << 10
)

var (
	// accountingScope must match the manifest; Dex Web also requests openid for the ID token.
	accountingScope = "com.intuit.quickbooks.accounting"
	// terminalTokenErrorCodes are the token endpoint errors that no retry can recover.
	terminalTokenErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client", "unsupported_grant_type"}
	// realmIDPattern matches a QuickBooks company ID, a decimal string such as 9130354890937306.
	realmIDPattern = regexp.MustCompile(`^[1-9][0-9]{0,24}$`)

	errCredentialRefreshUnavailable = errors.New("QuickBooks access token refresh is temporarily unavailable")
	// errRealmIDMissing means the ID token is absent or has no realmid claim, so the realmId setting may supply it.
	errRealmIDMissing = errors.New("QuickBooks ID token does not name the company")
)

// CredentialRefreshDriver exchanges Intuit's rotating refresh token for a new one-hour access
// token, presenting the app as HTTP Basic credentials as Intuit documents. It calls Intuit's
// token endpoint but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// idTokenClaims are the ID token claims read; decoding matches both the realmid and realmId spellings.
type idTokenClaims struct {
	RealmID  string          `json:"realmid"`
	Issuer   string          `json:"iss"`
	Audience json.RawMessage `json:"aud"`
}

// NewCredentialRefreshDriver constructs the provider-specific refresh driver.
// A nil HTTP client uses an 8-second client; the caller retains ownership of a supplied
// client. The token exchange never follows a redirect.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	return newCredentialRefreshDriver(httpClient, time.Now)
}

// newCredentialRefreshDriver lets the Client share its injected clock with the driver.
func newCredentialRefreshDriver(httpClient *http.Client, now func() time.Time) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: credentialRefreshTimeout}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: now}
}

// RefreshRequired reports whether the access token is absent, has no recorded expiry, or
// expires within five minutes. Intuit access tokens last one hour.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh exchanges the refresh token and stores the refresh token Intuit returns, which
// Intuit may rotate on any refresh, or keeps the prior one if Intuit omits it. The ID token
// from consent is kept, because it names the company the tokens are bound to. A rejected
// client or grant returns an error wrapped by sdkgo.NewReauthorizationRequiredError; every
// other failure is retryable.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("QuickBooks credential refresh driver is not configured")
	}
	ctx, cancel := context.WithTimeout(ctx, credentialRefreshTimeout)
	defer cancel()
	credentials := state.Credentials
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:       "Intuit",
		URL:                intuitTokenEndpoint,
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: terminalTokenErrorCodes,
	}
	client := oauthtoken.ClientCredentials{
		ID: credentials.ClientID, Secret: credentials.ClientSecret, AuthenticationMethod: oauthtoken.ClientSecretBasic,
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, client, credentials.RefreshToken)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return sdkgo.CredentialRefreshResult[Credentials]{}, err
		}
		return sdkgo.CredentialRefreshResult[Credentials]{}, fmt.Errorf("%w: %w", errCredentialRefreshUnavailable, err)
	}
	if token.Scope != "" && !oauthtoken.HasAllScopes(token.Scope, []string{accountingScope}) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(
			errors.New("Intuit granted a token without the com.intuit.quickbooks.accounting scope"))
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}

// validateResolvedCredentials checks only what a request needs, so credentials without renewal material pass.
func validateResolvedCredentials(credentials Credentials) error {
	if !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return errors.New("QuickBooks access token is missing or is not printable ASCII without spaces")
	}
	return nil
}

// resolveRealmID prefers the ID token's company, falls back to realmId, and refuses a conflicting realmId.
func resolveRealmID(credentials Credentials, configuredRealmID string) (string, error) {
	tokenRealmID, err := realmIDFromIDToken(credentials.IDToken, credentials.ClientID)
	switch {
	case err == nil && configuredRealmID != "" && configuredRealmID != tokenRealmID:
		return "", errors.New("the realmId setting names another company than the one Intuit authorized: clear it or authorize that company")
	case err == nil:
		return tokenRealmID, nil
	case errors.Is(err, errRealmIDMissing) && configuredRealmID != "":
		return configuredRealmID, nil
	case errors.Is(err, errRealmIDMissing):
		return "", errors.New("QuickBooks company ID is unknown: authorize again while signed in to the company, or enter its company ID in realmId")
	default:
		return "", err
	}
}

// realmIDFromIDToken reads Intuit's realmid after checking issuer and audience; OIDC Core 3.1.3.7 lets TLS replace the signature check.
func realmIDFromIDToken(idToken sdkgo.SecretString, clientID string) (string, error) {
	token := idToken.Reveal()
	switch {
	case token == "":
		return "", errRealmIDMissing
	case len(token) > maximumIDTokenBytes:
		return "", errors.New("QuickBooks ID token is too large")
	}
	segments := strings.Split(token, ".")
	if len(segments) != 3 {
		return "", errors.New("QuickBooks ID token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(segments[1], "="))
	if err != nil {
		return "", errors.New("QuickBooks ID token payload is not base64url")
	}
	var claims idTokenClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", errors.New("QuickBooks ID token payload is not a JSON object")
	}
	switch {
	case claims.Issuer != intuitIssuer:
		return "", errors.New("QuickBooks ID token was not issued by Intuit")
	case !hasAudience(claims.Audience, clientID):
		return "", errors.New("QuickBooks ID token was issued to another app")
	case claims.RealmID == "":
		return "", errRealmIDMissing
	case !realmIDPattern.MatchString(claims.RealmID):
		return "", errors.New("QuickBooks ID token realmid is not a company ID")
	}
	return claims.RealmID, nil
}

// hasAudience accepts an aud claim that is the client ID or a list containing it.
func hasAudience(audience json.RawMessage, clientID string) bool {
	if clientID == "" {
		return false
	}
	var single string
	if json.Unmarshal(audience, &single) == nil {
		return single == clientID
	}
	var several []string
	if json.Unmarshal(audience, &several) != nil {
		return false
	}
	for _, candidate := range several {
		if candidate == clientID {
			return true
		}
	}
	return false
}
