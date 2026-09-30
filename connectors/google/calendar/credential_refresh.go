// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package calendar

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
)

// calendarOAuthScopes must match the manifest scopes and the scopes an administrator delegates.
var calendarOAuthScopes = []string{
	"https://www.googleapis.com/auth/calendar.events",
	"https://www.googleapis.com/auth/calendar.events.freebusy",
	"https://www.googleapis.com/auth/calendar.calendarlist.readonly",
}

// googleTerminalRefreshErrorCodes are the Google token endpoint errors that no retry can recover.
var googleTerminalRefreshErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client"}

// CredentialRefreshDriver refreshes Google OAuth and Workspace delegated credentials.
// It performs provider calls but leaves locking and atomic persistence to the credential provider.
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

// RefreshRequired reports whether the access credential is absent, has no recorded expiry,
// or expires within five minutes.
func (driver *CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh obtains a replacement access credential for the selected authorization method.
// Terminal Google responses and credentials missing a Calendar scope require reauthorization.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Google Calendar credential refresh driver is not configured")
	}
	switch state.Credentials.AuthMethodID {
	case "", GoogleOAuthAuthMethodID:
		return driver.refreshGoogleOAuth(ctx, state.Credentials)
	case WorkspaceDomainDelegationAuthMethodID:
		return driver.refreshWorkspaceDelegation(ctx, state.Credentials)
	default:
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Google Calendar authorization method is not supported")
	}
}

// DecodeCredentialsJSON decodes trusted broker credential material using connector validation.
func DecodeCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	return decodeLocalCredentials(contents)
}

// DecodeResolvedCredentialsJSON decodes an operation-scoped credential returned by the hosted broker.
// Renewal material is intentionally absent from this short-lived representation and is rejected.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		AuthMethodID string `json:"auth_method"`
		AccessToken  string `json:"access_token"`
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fields); err != nil {
		return Credentials{}, errors.New("Google Calendar resolved credential is invalid")
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

func (driver *CredentialRefreshDriver) googleTokenEndpoint(tokenEndpointURL string) oauthtoken.TokenEndpoint {
	return oauthtoken.TokenEndpoint{
		ProviderName:       "Google",
		URL:                tokenEndpointURL,
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: googleTerminalRefreshErrorCodes,
	}
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
	if address, err := mail.ParseAddress(delegatedUser); err != nil || address.Name != "" || address.Address != delegatedUser {
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
		Scope:    strings.Join(calendarOAuthScopes, " "),
		IssuedAt: now,
		Lifetime: time.Hour,
	}, privateKey)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Workspace service-account assertion could not be signed")
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

// parseServiceAccountKey decodes the fields the connector uses from a Google service-account JSON key.
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

// validateReturnedScopes accepts an omitted scope, which Google does not always return, or a complete grant.
func validateReturnedScopes(scope string) error {
	if strings.TrimSpace(scope) == "" || oauthtoken.HasAllScopes(scope, calendarOAuthScopes) {
		return nil
	}
	return errors.New("Google credential lacks a required Calendar scope; reconnect and keep every requested scope checked")
}

func validateResolvedCredentials(credentials Credentials) error {
	if credentials.AccessToken.Reveal() == "" {
		return errors.New("Google Calendar access token is required")
	}
	switch credentials.AuthMethodID {
	case "", GoogleOAuthAuthMethodID, WorkspaceDomainDelegationAuthMethodID:
		return nil
	default:
		return errors.New("Google Calendar authorization method is invalid")
	}
}
