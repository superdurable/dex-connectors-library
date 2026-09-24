// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package linkedinconnector provides a bounded LinkedIn OpenID Connect UserInfo query.
package linkedinconnector

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	connector "github.com/superdurable/dex-connectors-library/sdk/go"
)

const userAgent = "superdurable-dex-linkedin-connector/0.1"

type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
}

func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

type Client struct {
	userInfoURL      *url.URL
	maxResponseBytes int64
	httpClient       *http.Client
	credentials      connector.CredentialProvider[Credentials]
}

type GetAuthenticatedProfileInput struct{}

type AuthenticatedProfile struct {
	Subject       string `json:"subject"`
	Name          string `json:"name,omitempty"`
	GivenName     string `json:"givenName,omitempty"`
	FamilyName    string `json:"familyName,omitempty"`
	PictureURL    string `json:"pictureUrl,omitempty"`
	Locale        string `json:"locale,omitempty"`
	VerifiedEmail string `json:"verifiedEmail"`
}

type GetAuthenticatedProfileOperation struct{ client *Client }

type userInfo struct {
	Subject       string          `json:"sub"`
	Name          string          `json:"name"`
	GivenName     string          `json:"given_name"`
	FamilyName    string          `json:"family_name"`
	Picture       string          `json:"picture"`
	Locale        json.RawMessage `json:"locale"`
	Email         string          `json:"email"`
	EmailVerified bool            `json:"email_verified"`
}

type providerResponse struct {
	statusCode int
	header     http.Header
	body       []byte
	requestID  string
}

var errResponseTooLarge = errors.New("LinkedIn response exceeds the configured size limit")

func New(config Config, credentials connector.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	userInfoURL, err := url.Parse(config.UserInfoURL)
	if err != nil || userInfoURL.Scheme == "" || userInfoURL.Hostname() == "" {
		return nil, fmt.Errorf("LinkedIn UserInfo URL must be absolute")
	}
	if userInfoURL.Scheme != "https" && !isLoopback(userInfoURL.Hostname()) {
		return nil, fmt.Errorf("non-loopback LinkedIn UserInfo URL must use HTTPS")
	}
	if userInfoURL.User != nil || userInfoURL.RawQuery != "" || userInfoURL.Fragment != "" {
		return nil, fmt.Errorf("LinkedIn UserInfo URL cannot contain user info, a query, or a fragment")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	dependencies := clientOptions{}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("LinkedIn connector option is nil")
		}
		option(&dependencies)
	}
	httpClient := dependencies.httpClient
	if httpClient == nil {
		httpClient = &http.Client{Timeout: config.Timeout}
	} else {
		copy := *httpClient
		httpClient = &copy
		if httpClient.Timeout == 0 {
			httpClient.Timeout = config.Timeout
		}
	}
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		userInfoURL: userInfoURL, maxResponseBytes: config.MaxResponseBytes,
		httpClient: httpClient, credentials: credentials,
	}, nil
}

func (client *Client) GetAuthenticatedProfile() GetAuthenticatedProfileOperation {
	return GetAuthenticatedProfileOperation{client: client}
}

func (GetAuthenticatedProfileOperation) Definition() connector.QueryDefinition {
	return GetAuthenticatedProfileDefinition
}

func (operation GetAuthenticatedProfileOperation) Invoke(call connector.Call, _ GetAuthenticatedProfileInput) connector.QueryAttempt[AuthenticatedProfile] {
	credential, failure := operation.client.resolveCredential(call)
	if failure != nil {
		return connector.NewQueryBranch(GetAuthenticatedProfileBranchAuthorizationRevoked, AuthenticatedProfile{}, failure, connector.Receipt{})
	}
	response, err := operation.client.get(call, credential)
	if err != nil {
		if errors.Is(err, errResponseTooLarge) {
			failure := providerFailure(connector.FailureResponseTooLarge, "LinkedIn UserInfo response exceeds the configured size limit")
			return connector.NewQueryBranch(GetAuthenticatedProfileBranchFailed, AuthenticatedProfile{}, &failure, receipt(response))
		}
		return connector.NewQueryRetry[AuthenticatedProfile](providerFailure(connector.FailureAvailability, "LinkedIn is unavailable"), 0)
	}
	if attempt := classifyResponse(response); attempt != nil {
		return *attempt
	}
	var claims userInfo
	if err := decodeJSON(response.body, &claims); err != nil {
		failure := providerFailure(connector.FailureProtocol, "LinkedIn returned an invalid UserInfo response")
		return connector.NewQueryBranch(GetAuthenticatedProfileBranchFailed, AuthenticatedProfile{}, &failure, receipt(response))
	}
	subject := strings.TrimSpace(claims.Subject)
	if subject == "" || len(subject) > 255 || !utf8.ValidString(subject) {
		failure := providerFailure(connector.FailureProtocol, "LinkedIn returned an invalid UserInfo response")
		return connector.NewQueryBranch(GetAuthenticatedProfileBranchFailed, AuthenticatedProfile{}, &failure, receipt(response))
	}
	email := verifiedEmail(claims.Email, claims.EmailVerified)
	if email == "" {
		failure := providerFailure(connector.FailureAuthentication, "LinkedIn verified email is required")
		return connector.NewQueryBranch(GetAuthenticatedProfileBranchVerifiedEmailRequired, AuthenticatedProfile{}, &failure, receipt(response))
	}
	profile := AuthenticatedProfile{
		Subject: subject, Name: boundedString(claims.Name, 256),
		GivenName: boundedString(claims.GivenName, 128), FamilyName: boundedString(claims.FamilyName, 128),
		PictureURL: safeURL(claims.Picture), Locale: normalizeLocale(claims.Locale), VerifiedEmail: email,
	}
	return connector.NewQueryBranch(GetAuthenticatedProfileBranchProfileLoaded, profile, nil, receipt(response))
}

func (client *Client) resolveCredential(call connector.Call) (Credentials, *connector.Failure) {
	credential, err := client.credentials.Resolve(call)
	if err != nil || credential.Validate() != nil {
		failure := providerFailure(connector.FailureAuthentication, "LinkedIn authorization is unavailable or revoked")
		return Credentials{}, &failure
	}
	return credential, nil
}

func (client *Client) get(call connector.Call, credential Credentials) (providerResponse, error) {
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, client.userInfoURL.String(), nil)
	if err != nil {
		return providerResponse{}, err
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Authorization", "Bearer "+credential.AccessToken.Reveal())
	request.Header.Set("User-Agent", userAgent)
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{}, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	if err != nil {
		return providerResponse{}, err
	}
	result := providerResponse{
		statusCode: response.StatusCode, header: response.Header.Clone(), body: body,
		requestID: safeRequestID(response.Header),
	}
	if int64(len(body)) > client.maxResponseBytes {
		result.body = nil
		return result, errResponseTooLarge
	}
	return result, nil
}

func classifyResponse(response providerResponse) *connector.QueryAttempt[AuthenticatedProfile] {
	receipt := receipt(response)
	if response.statusCode >= 200 && response.statusCode < 300 {
		return nil
	}
	if response.statusCode == http.StatusTooManyRequests {
		attempt := connector.NewQueryRetry[AuthenticatedProfile](
			providerFailure(connector.FailureRateLimit, "LinkedIn rate limit was reached"), retryDelay(response.header),
		)
		return &attempt
	}
	var branch connector.BranchID
	var failure connector.Failure
	switch response.statusCode {
	case http.StatusUnauthorized:
		branch = GetAuthenticatedProfileBranchAuthorizationRevoked
		failure = providerFailure(connector.FailureAuthentication, "LinkedIn authorization is invalid or revoked")
	case http.StatusForbidden:
		branch = GetAuthenticatedProfileBranchInsufficientScope
		failure = providerFailure(connector.FailureAuthorization, "LinkedIn authorization is forbidden or lacks scope")
	case http.StatusNotFound:
		branch = GetAuthenticatedProfileBranchNotFound
		failure = providerFailure(connector.FailureNotFound, "LinkedIn member was not found")
	default:
		if response.statusCode >= 500 {
			attempt := connector.NewQueryRetry[AuthenticatedProfile](
				providerFailure(connector.FailureAvailability, "LinkedIn is unavailable"), 0,
			)
			return &attempt
		}
		branch = GetAuthenticatedProfileBranchFailed
		failure = providerFailure(connector.FailureProviderRejection, "LinkedIn rejected the UserInfo request")
	}
	attempt := connector.NewQueryBranch(branch, AuthenticatedProfile{}, &failure, receipt)
	return &attempt
}

func receipt(response providerResponse) connector.Receipt {
	return connector.Receipt{ProviderRequestID: response.requestID}
}

func providerFailure(kind connector.FailureKind, message string) connector.Failure {
	return connector.Failure{Kind: kind, Provider: "linkedin", Operation: "getAuthenticatedProfile", Message: message}
}

func decodeJSON(body []byte, destination any) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	if err := decoder.Decode(destination); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return fmt.Errorf("response contains trailing JSON")
	}
	return nil
}

func verifiedEmail(value string, verified bool) string {
	if !verified {
		return ""
	}
	email := strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(email)
	if err != nil || !strings.EqualFold(address.Address, email) || len(email) > 320 {
		return ""
	}
	return email
}

func normalizeLocale(raw json.RawMessage) string {
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return ""
	}
	var value string
	if json.Unmarshal(raw, &value) == nil {
		return boundedString(value, 64)
	}
	var valueObject struct {
		Language string `json:"language"`
		Country  string `json:"country"`
	}
	if json.Unmarshal(raw, &valueObject) != nil {
		return ""
	}
	language := boundedString(valueObject.Language, 16)
	country := boundedString(valueObject.Country, 16)
	if language == "" {
		return country
	}
	if country == "" {
		return language
	}
	return language + "-" + country
}

func boundedString(value string, maximum int) string {
	value = strings.TrimSpace(value)
	if len(value) <= maximum {
		return value
	}
	value = value[:maximum]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func safeURL(value string) string {
	value = boundedString(value, 2048)
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme != "https" || parsed.Hostname() == "" || parsed.User != nil {
		return ""
	}
	return parsed.String()
}

func safeRequestID(header http.Header) string {
	for _, name := range []string{"X-LI-UUID", "X-RestLi-Id", "X-Request-Id"} {
		value := strings.TrimSpace(header.Get(name))
		if value != "" && len(value) <= 128 && strings.IndexFunc(value, func(character rune) bool {
			return !(character == '-' || character == '_' || character == '.' || character == ':' || character == '/' || character >= '0' && character <= '9' || character >= 'A' && character <= 'Z' || character >= 'a' && character <= 'z')
		}) == -1 {
			return value
		}
	}
	return ""
}

func retryDelay(header http.Header) time.Duration {
	seconds, err := strconv.ParseInt(header.Get("Retry-After"), 10, 64)
	if err == nil && seconds > 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Minute
}

func isLoopback(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}
