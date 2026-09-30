// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package spreadsheet

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const (
	googleOAuthTokenEndpoint = "https://oauth2.googleapis.com/token"
	credentialRefreshSkew    = oauthtoken.RefreshSkew
	googleDriveFileScope     = "https://www.googleapis.com/auth/drive.file"
)

// googleTerminalRefreshErrorCodes are the Google token endpoint errors that no retry can recover.
var googleTerminalRefreshErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client"}

// CredentialRefreshDriver refreshes Google Sheets OAuth credentials.
// It performs provider calls but leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
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

// Refresh exchanges the stored refresh token for a replacement access token.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Google Sheets credential refresh driver is not configured")
	}
	credentials := state.Credentials
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:       "Google",
		URL:                googleOAuthTokenEndpoint,
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: googleTerminalRefreshErrorCodes,
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
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

// DecodeCredentialsJSON decodes trusted broker credential material using connector validation.
func DecodeCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	return decodeLocalCredentials(contents)
}

// DecodeResolvedCredentialsJSON decodes an operation-scoped broker credential without renewal material.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		AccessToken string `json:"access_token"`
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fields); err != nil {
		return Credentials{}, errors.New("Google Sheets resolved credential is invalid")
	}
	credentials := Credentials{AccessToken: sdkgo.NewSecretString(fields.AccessToken)}
	return credentials, validateResolvedCredentials(credentials)
}

// EncodeCredentialsJSON encodes complete credential material for trusted atomic persistence.
func EncodeCredentialsJSON(credentials Credentials) ([]byte, error) {
	if err := credentials.Validate(); err != nil {
		return nil, err
	}
	return encodeLocalCredentials(credentials)
}
