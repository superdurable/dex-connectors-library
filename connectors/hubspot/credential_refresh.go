// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package hubspot

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

// hubspotOAuthTokenEndpoint is the 2026-09 token endpoint; the older /oauth/v1/token is deprecated on 2027-02-16.
const hubspotOAuthTokenEndpoint = "https://api.hubapi.com/oauth/2026-09/token"

// grantedScopesResponseField is HubSpot's JSON array of granted scopes, which replaces the RFC 6749 scope string.
const grantedScopesResponseField = "scopes"

var (
	// requiredOAuthScopes are the CRM scopes every operation and Studio picker of this connector needs.
	requiredOAuthScopes = []string{
		"crm.objects.contacts.read", "crm.objects.contacts.write",
		"crm.objects.companies.read", "crm.objects.companies.write",
		"crm.objects.deals.read", "crm.objects.deals.write",
		"crm.objects.owners.read",
	}
	// terminalRefreshErrorCodes are the token endpoint errors that no retry can recover.
	terminalRefreshErrorCodes = []string{"invalid_grant", "invalid_client", "unauthorized_client"}

	errCredentialRefreshUnavailable = errors.New("HubSpot OAuth token refresh is temporarily unavailable")
)

// CredentialRefreshDriver refreshes HubSpot OAuth access tokens. It calls
// HubSpot's token endpoint but leaves locking and atomic persistence to the
// credential provider. A private app token never needs a refresh.
type CredentialRefreshDriver struct {
	httpClient *http.Client
	now        func() time.Time
}

// NewCredentialRefreshDriver constructs the provider-specific refresh driver.
// A nil HTTP client uses a 25-second client; the caller retains ownership of a
// supplied client. The token exchange never follows a redirect.
func NewCredentialRefreshDriver(httpClient *http.Client) *CredentialRefreshDriver {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultRequestTimeout}
	}
	return &CredentialRefreshDriver{httpClient: httpClient, now: time.Now}
}

// RefreshRequired reports whether an OAuth access token is absent, has no
// recorded expiry, or expires within five minutes. It is always false for a
// private app token, which does not expire.
func (*CredentialRefreshDriver) RefreshRequired(state sdkgo.CredentialRefreshState[Credentials]) bool {
	if state.Credentials.AuthMethodID != OAuthAuthMethodID {
		return false
	}
	return oauthtoken.IsRefreshRequired(
		state.Credentials.AccessToken.Reveal() != "", state.ExpiresAt, state.Now, oauthtoken.RefreshWhenExpiryMissing,
	)
}

// Refresh exchanges the stored refresh token for a new 30-minute access token.
// HubSpot does not document refresh-token rotation, so the result always
// carries the next refresh token: the returned one, or the prior one when
// HubSpot omits it. A rejected grant, or a grant without the required CRM
// scopes, returns an error wrapped by sdkgo.NewReauthorizationRequiredError;
// every other failure is retryable.
func (driver *CredentialRefreshDriver) Refresh(
	ctx context.Context,
	state sdkgo.CredentialRefreshState[Credentials],
) (sdkgo.CredentialRefreshResult[Credentials], error) {
	if driver == nil || driver.httpClient == nil || driver.now == nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("HubSpot credential refresh driver is not configured")
	}
	credentials := state.Credentials
	if credentials.AuthMethodID != OAuthAuthMethodID {
		return sdkgo.CredentialRefreshResult[Credentials]{}, errors.New("HubSpot private app tokens cannot be refreshed")
	}
	tokenEndpoint := oauthtoken.TokenEndpoint{
		ProviderName:           "HubSpot",
		URL:                    hubspotOAuthTokenEndpoint,
		HTTPClient:             driver.httpClient,
		TerminalErrorCodes:     terminalRefreshErrorCodes,
		RetainedResponseFields: []string{grantedScopesResponseField},
	}
	token, err := tokenEndpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
		ID:                   credentials.OAuthClientID,
		Secret:               credentials.OAuthClientSecret,
		AuthenticationMethod: oauthtoken.ClientSecretPost,
	}, credentials.RefreshToken)
	if err != nil {
		if sdkgo.IsReauthorizationRequired(err) {
			return sdkgo.CredentialRefreshResult[Credentials]{}, err
		}
		return sdkgo.CredentialRefreshResult[Credentials]{}, fmt.Errorf("%w: %w", errCredentialRefreshUnavailable, err)
	}
	if err := validateGrantedScopes(token.RetainedFields[grantedScopesResponseField]); err != nil {
		return sdkgo.CredentialRefreshResult[Credentials]{}, sdkgo.NewReauthorizationRequiredError(err)
	}
	credentials.AccessToken = token.AccessToken
	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
	return sdkgo.CredentialRefreshResult[Credentials]{
		Credentials: credentials,
		ExpiresAt:   driver.now().UTC().Add(token.ExpiresIn),
	}, nil
}

// DecodeCredentialsJSON decodes trusted broker credential material using
// connector validation, including the selected auth_method.
func DecodeCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	return decodeLocalCredentials(contents)
}

// DecodeResolvedCredentialsJSON decodes an operation-scoped credential
// returned by the hosted broker: an access_token and an optional auth_method.
// Renewal material is rejected, because it never crosses the broker boundary.
// Without auth_method the connector treats the token as static and never asks
// the broker to refresh it after a rejection.
func DecodeResolvedCredentialsJSON(contents json.RawMessage) (Credentials, error) {
	var fields struct {
		AuthMethodID string `json:"auth_method"`
		AccessToken  string `json:"access_token"`
	}
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&fields); err != nil {
		return Credentials{}, errors.New("HubSpot resolved credential is invalid")
	}
	switch fields.AuthMethodID {
	case "", PrivateAppTokenAuthMethodID, OAuthAuthMethodID:
	default:
		return Credentials{}, errors.New("HubSpot resolved credential auth_method is invalid")
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

// validateGrantedScopes checks HubSpot's scopes array when the token response includes it.
func validateGrantedScopes(rawScopes json.RawMessage) error {
	if len(rawScopes) == 0 || bytes.Equal(bytes.TrimSpace(rawScopes), []byte("null")) {
		return nil
	}
	var grantedScopes []string
	if err := json.Unmarshal(rawScopes, &grantedScopes); err != nil {
		return errors.New("HubSpot returned an invalid scopes list")
	}
	if !oauthtoken.HasAllScopes(strings.Join(grantedScopes, " "), requiredOAuthScopes) {
		return errors.New("HubSpot OAuth grant lacks a required CRM scope")
	}
	return nil
}
