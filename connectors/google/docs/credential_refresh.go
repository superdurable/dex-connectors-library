// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package docs

import (
	"bytes"
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
	// delegationAssertionLifetime is Google's maximum service-account assertion lifetime.
	delegationAssertionLifetime = time.Hour
)

// docsOAuthScopes are the scopes both authorization methods request, in manifest order.
var docsOAuthScopes = []string{
	"https://www.googleapis.com/auth/documents",
	"https://www.googleapis.com/auth/drive.file",
	"https://www.googleapis.com/auth/drive.metadata.readonly",
}

// googleTerminalRefreshErrorCodes are the Google token endpoint errors that no retry can recover.
var googleTerminalRefreshErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client"}

// CredentialRefreshDriver refreshes Google OAuth and Workspace delegated Docs credentials.
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
		httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether the access token is absent, has no recorded
// expiry, or expires within oauthtoken.RefreshSkew. Google access tokens always expire.
func (driver *CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh obtains a replacement access token for the selected authorization
// method. Terminal Google grant errors, missing refresh material, and missing
// Docs or Drive scopes return an error wrapped by sdkgo.NewReauthorizationRequiredError.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Google Docs credential refresh driver is not configured")
	}
	switch state.Credentials.AuthMethodID {
	case "", GoogleOAuthAuthMethodID:
		return driver.refreshGoogleOAuth(ctx, state.Credentials)
	case WorkspaceDomainDelegationAuthMethodID:
		return driver.refreshWorkspaceDelegation(ctx, state.Credentials)
	default:
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Google Docs authorization method is not supported"))
	}
}

// DecodeCredentialsJSON decodes trusted broker credential material using connector validation.
func DecodeCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	return decodeLocalCredentials(contents)
}

// DecodeResolvedCredentialsJSON decodes an operation-scoped credential returned by the hosted broker.
// It accepts only the selected method and access token and rejects renewal material.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		AuthMethodID string `json:"auth_method"`
		AccessToken  string `json:"access_token"`
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fields); err != nil {
		return Credentials{}, errors.New("Google Docs resolved credential is invalid")
	}
	credentials := Credentials{AuthMethodID: fields.AuthMethodID, AccessToken: sdkgo.NewSecretString(fields.AccessToken)}
	return credentials, validateResolvedCredentials(credentials)
}

// EncodeCredentialsJSON encodes complete credential material for trusted atomic persistence.
func EncodeCredentialsJSON(credentials Credentials) ([]byte, error) {
	if err := credentials.Validate(); err != nil {
		return nil, err
	}
	return encodeLocalCredentials(credentials)
}

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
	if err := validateReturnedScopes(token.Scope); err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(err)
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}

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
		Scope:    strings.Join(docsOAuthScopes, " "),
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
	if err := validateReturnedScopes(token.Scope); err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(err)
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

// parseServiceAccountKey decodes the fields Docs delegation uses from a Google service-account JSON key.
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

// validateReturnedScopes accepts an omitted scope value, which Google sends for some delegated tokens.
func validateReturnedScopes(scope string) error {
	if strings.TrimSpace(scope) == "" || oauthtoken.HasAllScopes(scope, docsOAuthScopes) {
		return nil
	}
	return fmt.Errorf("Google credential lacks the required Docs and Drive scopes: %s", strings.Join(docsOAuthScopes, ", "))
}
