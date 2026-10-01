// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const (
	// ProductionOAuthAuthMethodID identifies the web server flow through login.salesforce.com.
	ProductionOAuthAuthMethodID = "salesforce-oauth"
	// SandboxOAuthAuthMethodID identifies the web server flow through test.salesforce.com.
	SandboxOAuthAuthMethodID = "salesforce-sandbox-oauth"
	// JWTBearerAuthMethodID identifies the server-to-server JWT bearer flow.
	JWTBearerAuthMethodID = "salesforce-jwt-bearer"

	productionLoginURL = "https://login.salesforce.com"
	sandboxLoginURL    = "https://test.salesforce.com"
	oauthTokenPath     = "/services/oauth2/token"
	instanceURLField   = "instance_url"
	// nominalSessionLifetime is Salesforce's default session timeout; a shorter org timeout surfaces as one 401.
	nominalSessionLifetime = 2 * time.Hour
	// jwtAssertionLifetime stays inside the three minutes Salesforce allows between iat and exp.
	jwtAssertionLifetime = 2 * time.Minute
)

// salesforceSessionScopes lists scopes that each grant REST API access; a token needs one of them.
var salesforceSessionScopes = []string{"api", "full"}

// salesforceTerminalRefreshErrorCodes are token endpoint errors that no retry can recover.
var salesforceTerminalRefreshErrorCodes = []string{
	"inactive_org", "inactive_user", "invalid_client", "invalid_client_credentials", "invalid_client_id",
	"invalid_grant", "unauthorized_client", "unsupported_grant_type",
}

// CredentialRefreshDriver refreshes Salesforce OAuth sessions and mints JWT
// bearer sessions. It performs token endpoint calls but leaves locking and
// atomic persistence to the credential provider.
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

// RefreshRequired reports whether the session token or instance URL is absent,
// or the recorded expiry is within oauthtoken.RefreshSkew. Salesforce returns no
// token lifetime, so a session without a recorded expiry is kept until
// Salesforce rejects it with 401 INVALID_SESSION_ID.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	hasSession := state.Credentials.AccessToken.Reveal() != "" && state.Credentials.InstanceURL != ""
	return oauthtoken.IsRefreshRequired(hasSession, state.ExpiresAt, state.Now, oauthtoken.KeepWhenExpiryMissing)
}

// Refresh obtains a replacement session for the selected authorization method
// and records the instance URL Salesforce returns with it. Terminal token
// endpoint errors, missing refresh or signing material, and a token without
// the api scope return an error wrapped by sdkgo.NewReauthorizationRequiredError.
// A new refresh token is stored only when the app rotates refresh tokens.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Salesforce credential refresh driver is not configured")
	}
	switch state.Credentials.AuthMethodID {
	case "", ProductionOAuthAuthMethodID:
		return driver.refreshOAuthSession(ctx, state.Credentials, productionLoginURL)
	case SandboxOAuthAuthMethodID:
		return driver.refreshOAuthSession(ctx, state.Credentials, sandboxLoginURL)
	case JWTBearerAuthMethodID:
		return driver.mintJWTBearerSession(ctx, state.Credentials)
	default:
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Salesforce authorization method is not supported"))
	}
}

// DecodeCredentialsJSON decodes trusted broker credential material using connector validation.
func DecodeCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	return decodeLocalCredentials(contents)
}

// DecodeResolvedCredentialsJSON decodes an operation-scoped credential returned
// by the hosted broker. It accepts only the selected method, session token, and
// instance URL, and rejects refresh tokens, client secrets, and private keys.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		AuthMethodID string `json:"auth_method"`
		AccessToken  string `json:"access_token"`
		InstanceURL  string `json:"instance_url"`
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fields); err != nil {
		return Credentials{}, errors.New("Salesforce resolved credential is invalid")
	}
	credentials := Credentials{
		AuthMethodID: fields.AuthMethodID, AccessToken: sdkgo.NewSecretString(fields.AccessToken), InstanceURL: fields.InstanceURL,
	}
	return credentials, validateResolvedCredentials(credentials)
}

// EncodeCredentialsJSON encodes complete credential material for trusted atomic persistence.
func EncodeCredentialsJSON(credentials Credentials) ([]byte, error) {
	if err := credentials.Validate(); err != nil {
		return nil, err
	}
	return encodeLocalCredentials(credentials)
}

func (driver *CredentialRefreshDriver) refreshOAuthSession(
	ctx context.Context,
	credentials Credentials,
	loginURL string,
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	token, err := driver.tokenEndpoint(loginURL).ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.OAuthClientID,
		Secret:               credentials.OAuthClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, credentials.RefreshToken)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	session, err := driver.acceptSession(token, credentials)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	session.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{Credentials: session, ExpiresAt: driver.now().UTC().Add(nominalSessionLifetime)}, nil
}

func (driver *CredentialRefreshDriver) mintJWTBearerSession(
	ctx context.Context,
	credentials Credentials,
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	clientID, username := strings.TrimSpace(credentials.OAuthClientID), strings.TrimSpace(credentials.JWTUsername)
	if clientID == "" || username == "" {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Salesforce JWT bearer consumer key or username is missing"))
	}
	loginURL := productionLoginURL
	switch credentials.JWTLoginEnvironment {
	case "", JWTLoginEnvironmentProduction:
	case JWTLoginEnvironmentSandbox:
		loginURL = sandboxLoginURL
	default:
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Salesforce JWT bearer login environment is invalid"))
	}
	privateKey, err := oauthtoken.ParseRSAPrivateKeyPEM(credentials.JWTPrivateKey.Reveal())
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(fmt.Errorf("Salesforce JWT bearer %w", err))
	}
	now := driver.now().UTC()
	assertion, err := oauthtoken.SignJWTBearerAssertion(oauthtoken.JWTBearerAssertion{
		Issuer: clientID, Subject: username, Audience: loginURL, IssuedAt: now, Lifetime: jwtAssertionLifetime,
	}, privateKey)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("Salesforce JWT bearer assertion could not be signed"))
	}
	token, err := driver.tokenEndpoint(loginURL).ExchangeJWTBearerAssertion(ctx, assertion)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	session, err := driver.acceptSession(token, credentials)
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	return sdkgo.CredentialRefreshResult[Credentials]{Credentials: session, ExpiresAt: now.Add(nominalSessionLifetime)}, nil
}

// acceptSession checks the granted scope and stores the new token and instance URL.
// A response without instance_url keeps the prior one; a response with an invalid one is retried.
func (driver *CredentialRefreshDriver) acceptSession(token oauthtoken.TokenResponse, credentials Credentials) (Credentials, error) {
	if strings.TrimSpace(token.Scope) != "" && !hasSessionScope(token.Scope) {
		return Credentials{}, sdkgo.NewReauthorizationRequiredError(errors.New("Salesforce session lacks the api scope"))
	}
	instanceURL := credentials.InstanceURL
	if rawInstanceURL, isPresent := token.RetainedFields[instanceURLField]; isPresent {
		if err := json.Unmarshal(rawInstanceURL, &instanceURL); err != nil {
			return Credentials{}, errors.New("Salesforce token response instance_url is not a string")
		}
	}
	if err := validateInstanceURL(instanceURL); err != nil {
		return Credentials{}, errors.New("Salesforce token response lacks a valid instance_url")
	}
	credentials.AccessToken = token.AccessToken
	credentials.InstanceURL = strings.TrimRight(instanceURL, "/")
	return credentials, nil
}

func (driver *CredentialRefreshDriver) tokenEndpoint(loginURL string) oauthtoken.TokenEndpoint {
	return oauthtoken.TokenEndpoint{
		ProviderName:            "Salesforce",
		URL:                     loginURL + oauthTokenPath,
		HTTPClient:              driver.httpClient,
		TerminalErrorCodes:      salesforceTerminalRefreshErrorCodes,
		AcceptsMissingExpiresIn: true,
		RetainedResponseFields:  []string{instanceURLField},
	}
}

func hasSessionScope(scope string) bool {
	for _, sessionScope := range salesforceSessionScopes {
		if oauthtoken.HasAllScopes(scope, []string{sessionScope}) {
			return true
		}
	}
	return false
}
