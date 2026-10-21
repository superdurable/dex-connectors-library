// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package youtube

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const (
	googleOAuthTokenEndpoint = "https://oauth2.googleapis.com/token"
	credentialRefreshSkew    = oauthtoken.RefreshSkew
	youtubeScope             = "https://www.googleapis.com/auth/youtube"
	youtubeReadOnlyScope     = "https://www.googleapis.com/auth/youtube.readonly"

	// ReadOnlyAuthMethodID is the recommended method; it requests youtube.readonly and serves every read.
	ReadOnlyAuthMethodID = "google-oauth-readonly"
	// ManageAuthMethodID requests the youtube scope that addPlaylistItem requires.
	ManageAuthMethodID = "google-oauth-manage"
)

// requiredScope names the scope a refreshed token must carry for the method; a blank method is the manifest default.
func requiredScope(authMethodID string) (string, bool) {
	switch authMethodID {
	case "", ReadOnlyAuthMethodID:
		return youtubeReadOnlyScope, true
	case ManageAuthMethodID:
		return youtubeScope, true
	}
	return "", false
}

// googleTerminalRefreshErrorCodes are the Google token endpoint errors that no retry can recover.
var googleTerminalRefreshErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client"}

// CredentialRefreshDriver refreshes YouTube OAuth credentials.
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
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("YouTube credential refresh driver is not configured")
	}
	credentials := state.Credentials
	scope, ok := requiredScope(credentials.AuthMethodID)
	if !ok {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("YouTube authorization method is not supported"))
	}
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
	// The youtube scope includes everything youtube.readonly allows, so either satisfies a read-only method.
	if token.Scope != "" && !oauthtoken.HasAllScopes(token.Scope, []string{scope}) && !oauthtoken.HasAllScopes(token.Scope, []string{youtubeScope}) {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(errors.New("YouTube credential lacks the scope its authorization method requires"))
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}
