// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package helpscout

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
)

const helpScoutOAuthTokenEndpoint = "https://api.helpscout.net/v2/oauth2/token"

// helpScoutTerminalTokenErrorCodes are RFC 6749 codes; Help Scout documents no token error codes.
var helpScoutTerminalTokenErrorCodes = []string{"invalid_client", "unauthorized_client"}

// CredentialRefreshDriver obtains Help Scout access tokens with the client credentials grant, which Help
// Scout documents for integrations with its own account. A token lasts two days, and the response carries
// no refresh token, so the driver obtains a new one the same way. It performs the provider exchange but
// leaves locking and atomic persistence to the credential provider.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the driver. A nil HTTP client uses a 25-second client; the caller
// keeps ownership of a supplied client.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: helpScoutRequestTimeout}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether the access token is absent or expires within five minutes. A token
// without a recorded expiry is kept until Help Scout answers 401, which forces a new one.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.KeepWhenExpiryMissing,
	)
}

// Refresh posts grant_type=client_credentials with the App ID and App Secret in the form body to
// https://api.helpscout.net/v2/oauth2/token, as Help Scout documents, and returns the new access token with
// the expiry from expires_in. Help Scout defines no scopes, so none are sent. A missing App ID or App
// Secret, or a rejected client, requires the connection's credentials to be replaced; any other failure
// is retryable.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("Help Scout credential refresh driver is not configured")
	}
	credentials := state.Credentials
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:       "Help Scout",
		URL:                helpScoutOAuthTokenEndpoint,
		HTTPClient:         driver.httpClient,
		TerminalErrorCodes: helpScoutTerminalTokenErrorCodes,
	}
	token, err := tokenEndpoint.ExchangeClientCredentials(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.AppID,
		Secret:               credentials.AppSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	})
	if err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, err
	}
	credentials.AccessToken = token.AccessToken
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}
