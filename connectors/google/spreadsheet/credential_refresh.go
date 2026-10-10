// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spreadsheet

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const (
	// GoogleOAuthAuthMethodID identifies interactive Google OAuth authorization.
	GoogleOAuthAuthMethodID = "google-oauth"
	// WorkspaceDomainDelegationAuthMethodID identifies administrator-approved service-account delegation.
	WorkspaceDomainDelegationAuthMethodID = "workspace-domain-delegation"

	googleOAuthTokenEndpoint = "https://oauth2.googleapis.com/token"
	credentialRefreshSkew    = oauthtoken.RefreshSkew
	googleDriveFileScope     = "https://www.googleapis.com/auth/drive.file"
	// delegationAssertionLifetime is Google's maximum service-account assertion lifetime.
	delegationAssertionLifetime = time.Hour
)

// workspaceDelegationScopes reach every spreadsheet the delegated user can open, and list them for the picker.
var workspaceDelegationScopes = []string{
	"https://www.googleapis.com/auth/spreadsheets",
	"https://www.googleapis.com/auth/drive.readonly",
}

// googleTerminalRefreshErrorCodes are the Google token endpoint errors that no retry can recover.
var googleTerminalRefreshErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client"}

// CredentialRefreshDriver refreshes Google OAuth and Workspace delegated Google Sheets credentials.
// It performs token endpoint calls but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

type serviceAccountKey struct {
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// NewCredentialRefreshDriver constructs the provider-specific refresh driver.
// A nil HTTP client uses a 25-second client; the caller retains ownership of a supplied client.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 25 * time.Second}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether the access credential is absent or expires within five minutes.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh obtains a replacement access token for the selected authorization
// method. Terminal Google grant errors, missing refresh material, and missing
// scopes return an error wrapped by sdkgo.NewReauthorizationRequiredError.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Google Sheets credential refresh driver is not configured")
	}
	switch state.Credentials.AuthMethodID {
	case "", GoogleOAuthAuthMethodID:
		return driver.refreshGoogleOAuth(ctx, state.Credentials)
	case WorkspaceDomainDelegationAuthMethodID:
		return driver.refreshWorkspaceDelegation(ctx, state.Credentials)
	default:
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Google Sheets authorization method is not supported"))
	}
}

// refreshGoogleOAuth exchanges the stored refresh token for a replacement access token.
func (driver *CredentialRefreshDriver) refreshGoogleOAuth(
	ctx context.Context,
	credentials Credentials,
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	token, err := driver.googleTokenEndpoint(googleOAuthTokenEndpoint).ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.OAuthClientID,
		Secret:               credentials.OAuthClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, credentials.RefreshToken)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	if token.Scope != "" && !oauthtoken.HasAllScopes(token.Scope, []string{googleDriveFileScope}) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Google Sheets credential lacks the required drive.file scope"))
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}

// refreshWorkspaceDelegation signs a service-account assertion for the delegated user and exchanges it.
func (driver *CredentialRefreshDriver) refreshWorkspaceDelegation(
	ctx context.Context,
	credentials Credentials,
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	key, err := parseServiceAccountKey(credentials.ServiceAccountKey.Reveal())
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(err)
	}
	delegatedUser := strings.TrimSpace(credentials.DelegatedUser)
	if address, err := mail.ParseAddress(delegatedUser); err != nil || address.Address != delegatedUser {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Workspace delegated user is invalid"))
	}
	privateKey, err := oauthtoken.ParseRSAPrivateKeyPEM(key.PrivateKey)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(fmt.Errorf("Workspace service-account %w", err))
	}
	now := driver.now().UTC()
	assertion, err := oauthtoken.SignJWTBearerAssertion(oauthtoken.JWTBearerAssertion{
		Issuer:   key.ClientEmail,
		Subject:  delegatedUser,
		Audience: key.TokenURI,
		Scope:    strings.Join(workspaceDelegationScopes, " "),
		IssuedAt: now,
		Lifetime: delegationAssertionLifetime,
	}, privateKey)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Workspace service-account assertion could not be signed"))
	}
	token, err := driver.googleTokenEndpoint(key.TokenURI).ExchangeJWTBearerAssertion(ctx, assertion)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	// Google omits the scope from some delegated tokens; a returned scope must include every requested one.
	if strings.TrimSpace(token.Scope) != "" && !oauthtoken.HasAllScopes(token.Scope, workspaceDelegationScopes) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(
			fmt.Errorf("Google credential lacks the required scopes: %s", strings.Join(workspaceDelegationScopes, ", ")))
	}
	credentials.AccessToken = token.AccessToken
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   now.Add(token.ExpiresIn),
	}, nil
}

func (driver *CredentialRefreshDriver) googleTokenEndpoint(tokenEndpointURL string) oauthtoken.TokenEndpoint {
	return oauthtoken.TokenEndpoint{
		ProviderName:       "Google",
		URL:                tokenEndpointURL,
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: googleTerminalRefreshErrorCodes,
	}
}

// parseServiceAccountKey decodes the fields delegation uses from a Google service-account JSON key.
func parseServiceAccountKey(raw string) (serviceAccountKey, error) {
	var key serviceAccountKey
	if err := json.Unmarshal([]byte(raw), &key); err != nil {
		return serviceAccountKey{}, errors.New("Workspace service-account key is invalid JSON")
	}
	if key.ClientEmail == "" || key.PrivateKey == "" || key.TokenURI == "" {
		return serviceAccountKey{}, errors.New("Workspace service-account key is incomplete")
	}
	endpoint, err := url.Parse(key.TokenURI)
	if err != nil || endpoint.Scheme != "https" || endpoint.Hostname() == "" {
		return serviceAccountKey{}, errors.New("Workspace service-account token endpoint is invalid")
	}
	return key, nil
}
