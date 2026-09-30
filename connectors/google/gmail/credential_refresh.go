// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gmail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"sort"
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

var gmailOAuthScopes = []string{
	"https://www.googleapis.com/auth/gmail.readonly",
	"https://www.googleapis.com/auth/gmail.send",
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

// RefreshRequired reports whether the access credential is absent or expires within five minutes.
func (driver *CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh obtains a replacement access credential for the selected authorization method.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Gmail credential refresh driver is not configured")
	}
	switch state.Credentials.AuthMethodID {
	case "", GoogleOAuthAuthMethodID:
		return driver.refreshGoogleOAuth(ctx, state.Credentials)
	case WorkspaceDomainDelegationAuthMethodID:
		return driver.refreshWorkspaceDelegation(ctx, state.Credentials)
	default:
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Gmail authorization method is not supported")
	}
}

// DecodeCredentialsJSON decodes trusted broker credential material using connector validation.
func DecodeCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	return decodeLocalCredentials(contents)
}

// DecodeResolvedCredentialsJSON decodes an operation-scoped credential returned by the hosted broker.
// Renewal material is intentionally absent from this short-lived representation.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		AuthMethodID string `json:"auth_method"`
		AccessToken  string `json:"access_token"`
		PrimaryEmail string `json:"primary_email"`
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fields); err != nil {
		return Credentials{}, errors.New("Gmail resolved credential is invalid")
	}
	credentials := Credentials{
		AuthMethodID: fields.AuthMethodID,
		AccessToken:  sdkgo.NewSecretString(fields.AccessToken),
		PrimaryEmail: fields.PrimaryEmail,
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
	if delegatedUser == "" || !strings.Contains(delegatedUser, "@") {
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
		Scope:    strings.Join(gmailOAuthScopes, " "),
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
	credentials.PrimaryEmail = delegatedUser
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   now.Add(token.ExpiresIn),
	}, nil
}

// parseServiceAccountKey decodes the fields Gmail uses from a Google service-account JSON key.
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

func validateReturnedScopes(scope string) error {
	if strings.TrimSpace(scope) == "" {
		return nil
	}
	granted := make(map[string]bool)
	for _, value := range strings.Fields(scope) {
		granted[value] = true
	}
	missing := make([]string, 0, len(gmailOAuthScopes))
	for _, required := range gmailOAuthScopes {
		if !granted[required] {
			missing = append(missing, required)
		}
	}
	if len(missing) != 0 {
		sort.Strings(missing)
		return fmt.Errorf("Google credential lacks required Gmail scopes: %s", strings.Join(missing, ", "))
	}
	return nil
}
