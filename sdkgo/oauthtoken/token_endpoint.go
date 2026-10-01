// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package oauthtoken holds the provider-neutral OAuth 2.0 token endpoint
// exchanges behind connector credential refresh drivers: the refresh-token grant
// (RFC 6749 section 6), the JWT-bearer assertion grant (RFC 7523) with RS256
// signing, bounded response reads, terminal-error classification, returned-scope
// checks, and the expiry-skew rule for deciding when to refresh.
//
// The package contains no provider host, error-code value, scope, or credential.
// A connector passes those in from its own constants and keeps its own typed
// sdkgo.CredentialRefreshDriver. The driver still owns which credential fields
// hold the tokens; this package replaces only the exchange mechanics.
//
// A driver typically builds its endpoint once and delegates each refresh:
//
//	endpoint := oauthtoken.TokenEndpoint{
//		ProviderName:       "Example",
//		URL:                exampleTokenEndpoint,
//		HTTPClient:         driver.httpClient,
//		TerminalErrorCodes: []string{"invalid_grant", "invalid_client"},
//	}
//	token, err := endpoint.ExchangeRefreshToken(ctx, oauthtoken.ClientCredentials{
//		ID:                   credentials.OAuthClientID,
//		Secret:               credentials.OAuthClientSecret,
//		AuthenticationMethod: oauthtoken.ClientSecretPost,
//	}, credentials.RefreshToken)
//	if err != nil {
//		return sdkgo.CredentialRefreshResult[Credentials]{}, err
//	}
//	credentials.AccessToken = token.AccessToken
//	credentials.RefreshToken = token.NextRefreshToken(credentials.RefreshToken)
package oauthtoken

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	refreshTokenGrantType       = "refresh_token"
	clientCredentialsGrantType  = "client_credentials"
	jwtBearerAssertionGrantType = "urn:ietf:params:oauth:grant-type:jwt-bearer"
	bearerTokenType             = "Bearer"
	maxTokenResponseBytes       = 1 << 20
)

// ClientAuthenticationMethod selects how a refresh-token exchange presents the
// OAuth client to the token endpoint. Values use the RFC 7591
// token_endpoint_auth_method names.
type ClientAuthenticationMethod string

const (
	// ClientSecretPost sends client_id and client_secret in the form body.
	ClientSecretPost ClientAuthenticationMethod = "client_secret_post"
	// ClientSecretBasic sends client_id and client_secret as HTTP Basic credentials.
	ClientSecretBasic ClientAuthenticationMethod = "client_secret_basic"
	// PublicClient sends only client_id in the form body, as a PKCE public client does.
	PublicClient ClientAuthenticationMethod = "none"
)

// ClientCredentials identifies the OAuth client for a refresh-token exchange.
type ClientCredentials struct {
	// ID is the non-secret OAuth client ID.
	ID string
	// Secret is the OAuth client secret. PublicClient ignores it.
	Secret sdkgo.SecretString
	// AuthenticationMethod selects where ID and Secret travel.
	AuthenticationMethod ClientAuthenticationMethod
}

// TokenEndpoint is one provider token endpoint and the connector's rules for its responses.
type TokenEndpoint struct {
	// ProviderName begins every error message, such as "Google" or "GitHub".
	ProviderName string
	// URL is the absolute HTTPS token endpoint without user information.
	URL string
	// HTTPClient performs the exchange. The endpoint uses a copy that never
	// follows redirects, so client secrets and assertions reach only URL. The
	// caller retains ownership of the client and its transport.
	HTTPClient *http.Client
	// TerminalErrorCodes are the provider's OAuth error codes meaning that the
	// stored grant can never succeed again, such as "invalid_grant". A response
	// below HTTP 500 carrying one returns an error wrapped by
	// sdkgo.NewReauthorizationRequiredError, whatever its status, because some
	// providers report errors with 200 or 403. Every other failure is retryable.
	TerminalErrorCodes []string
	// AcceptedTokenTypes lists the token_type values accepted, compared without
	// case. Empty accepts only "Bearer".
	AcceptedTokenTypes []string
	// AcceptsMissingTokenType accepts a successful response without token_type.
	AcceptsMissingTokenType bool
	// AcceptsMissingExpiresIn accepts a successful response without expires_in,
	// for providers such as Salesforce whose session lifetime is configured
	// outside the token response. TokenResponse.ExpiresIn is then zero and the
	// driver chooses the replacement expiry.
	AcceptsMissingExpiresIn bool
	// RetainedResponseFields names non-standard top-level response fields the
	// driver must persist, such as "instance_url" or "api_domain". Their raw
	// JSON values appear in TokenResponse.RetainedFields. The standard token
	// fields cannot be retained, so a secret never leaves its SecretString.
	RetainedResponseFields []string
	// UsesJSONRequestBody sends the grant as a JSON object instead of the RFC 6749
	// form encoding, for providers such as Atlassian and Zendesk that document
	// only JSON token requests.
	UsesJSONRequestBody bool
	// AdditionalRequestParameters are fixed, non-secret parameters added to every
	// grant, such as a requested lifetime. They cannot replace a grant, client,
	// or assertion parameter.
	AdditionalRequestParameters map[string]string
}

// TokenResponse is a successful token endpoint response.
type TokenResponse struct {
	// AccessToken is the replacement access token.
	AccessToken sdkgo.SecretString
	// RefreshToken is the rotated refresh token, empty when the provider kept the prior one.
	RefreshToken sdkgo.SecretString
	// ExpiresIn is the access token lifetime. It is positive, or zero only when
	// TokenEndpoint.AcceptsMissingExpiresIn accepted a response without expires_in.
	ExpiresIn time.Duration
	// RefreshTokenExpiresIn is the rotated refresh token lifetime, zero when the provider omits it.
	RefreshTokenExpiresIn time.Duration
	// Scope is the provider's returned scope value, empty when the provider omits it.
	Scope string
	// TokenType is the provider's returned token_type value.
	TokenType string
	// RetainedFields holds the raw JSON value of each TokenEndpoint.RetainedResponseFields
	// entry the provider returned. An omitted field is absent from the map.
	RetainedFields map[string]json.RawMessage
}

// NextRefreshToken returns the rotated refresh token, or prior when the
// provider omitted a replacement. A driver must persist the result, because a
// refresh result carries the complete replacement credential.
func (response TokenResponse) NextRefreshToken(prior sdkgo.SecretString) sdkgo.SecretString {
	if response.RefreshToken.Reveal() == "" {
		return prior
	}
	return response.RefreshToken
}

type tokenEndpointResponse struct {
	AccessToken           string `json:"access_token"`
	ExpiresIn             int64  `json:"expires_in"`
	RefreshToken          string `json:"refresh_token"`
	RefreshTokenExpiresIn int64  `json:"refresh_token_expires_in"`
	Scope                 string `json:"scope"`
	TokenType             string `json:"token_type"`
	ErrorCode             string `json:"error"`
}

// ExchangeRefreshToken exchanges refreshToken for a replacement access token.
// Missing client or refresh material returns an error wrapped by
// sdkgo.NewReauthorizationRequiredError without calling the provider.
func (endpoint TokenEndpoint) ExchangeRefreshToken(
	ctx context.Context,
	client ClientCredentials,
	refreshToken sdkgo.SecretString,
) (TokenResponse, error) {
	if client.ID == "" || refreshToken.Reveal() == "" {
		return TokenResponse{}, sdkgo.NewReauthorizationRequiredError(
			fmt.Errorf("%s OAuth refresh material is incomplete", endpoint.ProviderName))
	}
	form := url.Values{
		"grant_type":    {refreshTokenGrantType},
		"refresh_token": {refreshToken.Reveal()},
	}
	var basicAuthenticationClient *ClientCredentials
	switch client.AuthenticationMethod {
	case ClientSecretPost, ClientSecretBasic:
		if client.Secret.Reveal() == "" {
			return TokenResponse{}, sdkgo.NewReauthorizationRequiredError(
				fmt.Errorf("%s OAuth refresh material is incomplete", endpoint.ProviderName))
		}
		if client.AuthenticationMethod == ClientSecretPost {
			form.Set("client_id", client.ID)
			form.Set("client_secret", client.Secret.Reveal())
		} else {
			basicAuthenticationClient = &client
		}
	case PublicClient:
		form.Set("client_id", client.ID)
	default:
		return TokenResponse{}, fmt.Errorf("%s OAuth client authentication method is not supported", endpoint.ProviderName)
	}
	return endpoint.exchange(ctx, form, basicAuthenticationClient)
}

// ExchangeClientCredentials obtains an access token with the client credentials grant (RFC 6749
// section 4.4), for providers whose own-account integrations authenticate with a client ID and secret
// instead of a user's consent. scopes, when given, are sent space-separated. The client must use
// ClientSecretPost or ClientSecretBasic; a missing ID or secret returns an error wrapped by
// sdkgo.NewReauthorizationRequiredError without calling the provider. The response usually carries no
// refresh token: the driver obtains a new access token the same way when the old one expires.
func (endpoint TokenEndpoint) ExchangeClientCredentials(
	ctx context.Context,
	client ClientCredentials,
	scopes ...string,
) (TokenResponse, error) {
	if client.ID == "" || client.Secret.Reveal() == "" {
		return TokenResponse{}, sdkgo.NewReauthorizationRequiredError(
			fmt.Errorf("%s OAuth client credentials are incomplete", endpoint.ProviderName))
	}
	form := url.Values{"grant_type": {clientCredentialsGrantType}}
	if len(scopes) > 0 {
		form.Set("scope", strings.Join(scopes, " "))
	}
	var basicAuthenticationClient *ClientCredentials
	switch client.AuthenticationMethod {
	case ClientSecretPost:
		form.Set("client_id", client.ID)
		form.Set("client_secret", client.Secret.Reveal())
	case ClientSecretBasic:
		basicAuthenticationClient = &client
	default:
		return TokenResponse{}, fmt.Errorf("%s client credentials grant needs ClientSecretPost or ClientSecretBasic", endpoint.ProviderName)
	}
	return endpoint.exchange(ctx, form, basicAuthenticationClient)
}

// ExchangeJWTBearerAssertion exchanges a signed JWT-bearer assertion for an
// access token. Build the assertion with SignJWTBearerAssertion. The provider
// authenticates the assertion signature, so no client credentials are sent.
func (endpoint TokenEndpoint) ExchangeJWTBearerAssertion(
	ctx context.Context,
	assertion sdkgo.SecretString,
) (TokenResponse, error) {
	if assertion.Reveal() == "" {
		return TokenResponse{}, sdkgo.NewReauthorizationRequiredError(
			fmt.Errorf("%s JWT-bearer assertion is empty", endpoint.ProviderName))
	}
	return endpoint.exchange(ctx, url.Values{
		"grant_type": {jwtBearerAssertionGrantType},
		"assertion":  {assertion.Reveal()},
	}, nil)
}

// exchange posts form to the endpoint. A non-nil basicAuthenticationClient is
// sent as HTTP Basic credentials.
func (endpoint TokenEndpoint) exchange(
	ctx context.Context,
	form url.Values,
	basicAuthenticationClient *ClientCredentials,
) (TokenResponse, error) {
	if endpoint.HTTPClient == nil {
		return TokenResponse{}, fmt.Errorf("%s token endpoint has no HTTP client", endpoint.ProviderName)
	}
	if err := validateTokenEndpointURL(endpoint.URL); err != nil {
		return TokenResponse{}, fmt.Errorf("%s token endpoint is invalid: %w", endpoint.ProviderName, err)
	}
	for _, fieldName := range endpoint.RetainedResponseFields {
		if isStandardTokenResponseField(fieldName) {
			return TokenResponse{}, fmt.Errorf("%s token endpoint cannot retain the standard %s field", endpoint.ProviderName, fieldName)
		}
	}
	for parameterName, parameterValue := range endpoint.AdditionalRequestParameters {
		if isGrantRequestParameter(parameterName) {
			return TokenResponse{}, fmt.Errorf("%s token endpoint cannot replace the %s request parameter", endpoint.ProviderName, parameterName)
		}
		form.Set(parameterName, parameterValue)
	}
	requestBody, contentType, err := encodeGrantRequestBody(form, endpoint.UsesJSONRequestBody)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("%s token request could not be built", endpoint.ProviderName)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint.URL, strings.NewReader(requestBody))
	if err != nil {
		return TokenResponse{}, fmt.Errorf("%s token request could not be built", endpoint.ProviderName)
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("Accept", "application/json")
	if basicAuthenticationClient != nil {
		request.SetBasicAuth(basicAuthenticationClient.ID, basicAuthenticationClient.Secret.Reveal())
	}
	response, err := providerhttp.NewProviderHTTPClient(endpoint.HTTPClient, 0).Do(request)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("%s token endpoint is unavailable: %w", endpoint.ProviderName, err)
	}
	defer response.Body.Close()
	contents, err := providerhttp.ReadBoundedBody(response.Body, maxTokenResponseBytes)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("%s token response could not be read", endpoint.ProviderName)
	}
	var decoded tokenEndpointResponse
	if err := json.Unmarshal(contents, &decoded); err != nil {
		return TokenResponse{}, fmt.Errorf("%s token response is invalid", endpoint.ProviderName)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 || decoded.ErrorCode != "" {
		// A server failure is never terminal: some providers report invalid_grant during outages.
		if response.StatusCode < 500 && slices.Contains(endpoint.TerminalErrorCodes, decoded.ErrorCode) {
			// Only a declared terminal code is repeated; provider descriptions never are.
			return TokenResponse{}, sdkgo.NewReauthorizationRequiredError(
				fmt.Errorf("%s rejected credential refresh with %s", endpoint.ProviderName, decoded.ErrorCode))
		}
		return TokenResponse{}, fmt.Errorf("%s token endpoint returned HTTP %d", endpoint.ProviderName, response.StatusCode)
	}
	hasAcceptedExpiresIn := decoded.ExpiresIn > 0 || (decoded.ExpiresIn == 0 && endpoint.AcceptsMissingExpiresIn)
	if decoded.AccessToken == "" || !hasAcceptedExpiresIn || !endpoint.isAcceptedTokenType(decoded.TokenType) {
		return TokenResponse{}, fmt.Errorf("%s token response omitted required fields", endpoint.ProviderName)
	}
	retainedFields, err := endpoint.retainResponseFields(contents)
	if err != nil {
		return TokenResponse{}, err
	}
	refreshTokenExpiresIn := time.Duration(0)
	if decoded.RefreshTokenExpiresIn > 0 {
		refreshTokenExpiresIn = time.Duration(decoded.RefreshTokenExpiresIn) * time.Second
	}
	return TokenResponse{
		AccessToken:           sdkgo.NewSecretString(decoded.AccessToken),
		RefreshToken:          sdkgo.NewSecretString(decoded.RefreshToken),
		ExpiresIn:             time.Duration(decoded.ExpiresIn) * time.Second,
		RefreshTokenExpiresIn: refreshTokenExpiresIn,
		Scope:                 decoded.Scope,
		TokenType:             decoded.TokenType,
		RetainedFields:        retainedFields,
	}, nil
}

// retainResponseFields copies the declared non-standard fields from a successful response body.
func (endpoint TokenEndpoint) retainResponseFields(contents []byte) (map[string]json.RawMessage, error) {
	if len(endpoint.RetainedResponseFields) == 0 {
		return nil, nil
	}
	var topLevelFields map[string]json.RawMessage
	if err := json.Unmarshal(contents, &topLevelFields); err != nil {
		return nil, fmt.Errorf("%s token response is invalid", endpoint.ProviderName)
	}
	retainedFields := make(map[string]json.RawMessage, len(endpoint.RetainedResponseFields))
	for _, fieldName := range endpoint.RetainedResponseFields {
		if value, isPresent := topLevelFields[fieldName]; isPresent {
			retainedFields[fieldName] = value
		}
	}
	return retainedFields, nil
}

// encodeGrantRequestBody encodes single-valued grant parameters as a form or a JSON object.
func encodeGrantRequestBody(form url.Values, usesJSONRequestBody bool) (string, string, error) {
	if !usesJSONRequestBody {
		return form.Encode(), "application/x-www-form-urlencoded", nil
	}
	parameters := make(map[string]string, len(form))
	for parameterName := range form {
		parameters[parameterName] = form.Get(parameterName)
	}
	encoded, err := json.Marshal(parameters)
	if err != nil {
		return "", "", err
	}
	return string(encoded), "application/json", nil
}

func isGrantRequestParameter(parameterName string) bool {
	switch parameterName {
	case "grant_type", "refresh_token", "client_id", "client_secret", "assertion", "client_assertion", "client_assertion_type":
		return true
	default:
		return false
	}
}

func isStandardTokenResponseField(fieldName string) bool {
	switch fieldName {
	case "access_token", "refresh_token", "expires_in", "refresh_token_expires_in", "scope", "token_type", "error",
		"error_description", "error_uri", "id_token":
		return true
	default:
		return false
	}
}

func (endpoint TokenEndpoint) isAcceptedTokenType(tokenType string) bool {
	if tokenType == "" {
		return endpoint.AcceptsMissingTokenType
	}
	acceptedTokenTypes := endpoint.AcceptedTokenTypes
	if len(acceptedTokenTypes) == 0 {
		acceptedTokenTypes = []string{bearerTokenType}
	}
	for _, acceptedTokenType := range acceptedTokenTypes {
		if strings.EqualFold(tokenType, acceptedTokenType) {
			return true
		}
	}
	return false
}

// validateTokenEndpointURL requires an absolute HTTPS URL with a host and no
// user information, so a client secret or assertion is never sent in the clear.
// Error messages never repeat the value.
func validateTokenEndpointURL(value string) error {
	parsed, err := url.Parse(value)
	if err != nil {
		return errors.New("URL cannot be parsed")
	}
	if parsed.Scheme != "https" || parsed.Hostname() == "" {
		return errors.New("URL must be absolute HTTPS with a host")
	}
	if parsed.User != nil {
		return errors.New("URL cannot contain user information")
	}
	return nil
}
