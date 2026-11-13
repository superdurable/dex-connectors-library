// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package wecom sends WeCom (WeChat Work) messages through typed Dex Mutation Steps.
//
// A group-robot connection posts to the one group chat that owns its webhook key with
// sendGroupRobotMessage. An application connection sends as a self-built application to
// members, departments, and tags with sendApplicationMessage, exchanging its corp ID and
// secret for an access token that the Client caches in memory until shortly before expiry.
//
// Webhook keys, application secrets, and access tokens travel in WeCom query strings, so
// the connector never copies a request URL or transport error text into a Failure, log, or
// result. The runnable Flows under examples/ show both operations end to end.
package wecom

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	providerName = "wecom"

	groupRobotAuthMethodID  = "group-robot"
	applicationAuthMethodID = "application"
)

// Option supplies a non-serializable client dependency to New.
type Option interface{ applyClientOption(*Client) }

type httpClientOption struct{ httpClient *http.Client }

func (option httpClientOption) applyClientOption(client *Client) {
	client.httpClient = option.httpClient
}

// WithHTTPClient supplies the transport for WeCom requests. New copies the client, keeps its
// Transport, disables redirects, and applies the configured timeout when the client sets none.
// A Transport other than *http.Transport cannot prove that a failed group robot post never left
// the process, so every transport error from it selects uncertain instead of retrying.
func WithHTTPClient(httpClient *http.Client) Option { return httpClientOption{httpClient} }

// Client holds immutable WeCom settings, resolves credentials for each call, and caches one
// application access token per corp ID and secret. A Client is safe for concurrent Steps.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
	// hasStandardTransport is true for net/http's own Transport, whose dial errors prove nothing was sent.
	hasStandardTransport bool
	accessTokens         *accessTokenCache
	now                  func() time.Time
}

// New validates the connection settings without contacting WeCom. Credentials are resolved
// at each call, so a rotated webhook key or secret takes effect without a restart.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("WeCom credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, fmt.Errorf("WeCom timeout and maxResponseBytes must be positive")
	}
	baseURL, err := providerhttp.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("WeCom base URL: %w", err)
	}
	client := &Client{
		baseURL:          baseURL,
		maxResponseBytes: config.MaxResponseBytes,
		credentials:      credentials,
		accessTokens:     newAccessTokenCache(),
		now:              time.Now,
	}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("WeCom client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	client.hasStandardTransport = isStandardTransport(client.httpClient.Transport)
	return client, nil
}

// SendGroupRobotMessage returns the group robot post Mutation bound to this client.
func (client *Client) SendGroupRobotMessage() SendGroupRobotMessageOperation {
	return SendGroupRobotMessageOperation{client: client}
}

// SendApplicationMessage returns the application message Mutation bound to this client.
func (client *Client) SendApplicationMessage() SendApplicationMessageOperation {
	return SendApplicationMessageOperation{client: client}
}

func isStandardTransport(transport http.RoundTripper) bool {
	if transport == nil {
		return true
	}
	_, isStandard := transport.(*http.Transport)
	return isStandard
}

// isUnsentDialError reports a DNS, dial, or proxy CONNECT failure, which net/http's Transport
// returns before it writes any request byte to the WeCom connection.
func isUnsentDialError(err error) bool {
	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return true
	}
	var operationError *net.OpError
	return errors.As(err, &operationError) && (operationError.Op == "dial" || operationError.Op == "proxyconnect")
}

// WeCom error codes from https://developer.work.weixin.qq.com/document/path/90313.
const (
	errcodeSuccess = 0
	// errcodeSystemBusy (-1, 系统繁忙) means the server was unavailable and asks for a later retry.
	errcodeSystemBusy = -1
	// errcodeFrequencyLimited (45009, 接口调用超过限制) rejects a call over a frequency limit.
	errcodeFrequencyLimited = 45009
	// errcodeConcurrencyLimited (45033, 接口并发调用超过限制) rejects a call over the concurrency limit.
	errcodeConcurrencyLimited = 45033
	// errcodeInvalidSecret (40001, 不合法的secret参数) rejects a wrong, reset, or disabled application secret.
	errcodeInvalidSecret = 40001
	// errcodeInvalidCorpID (40013, 不合法的CorpID) rejects an unknown corp ID.
	errcodeInvalidCorpID = 40013
	// errcodeInvalidAccessToken (40014, 不合法的access_token) rejects an expired or revoked token.
	errcodeInvalidAccessToken = 40014
	// errcodeMissingAccessToken (41001, 缺少access_token参数) rejects a request without a token.
	errcodeMissingAccessToken = 41001
	// errcodeExpiredAccessToken (42001, access_token已过期) rejects an expired token.
	errcodeExpiredAccessToken = 42001
	// errcodeUntrustedIP (60020, 不安全的访问IP) rejects a caller outside the application's trusted IPs.
	errcodeUntrustedIP = 60020
	// errcodeNoPermission (48002, API接口无权限调用) rejects an application without API permission.
	errcodeNoPermission = 48002
	// errcodeAgentNotPermitted (301002, 无权限操作指定的应用) rejects a token for another application.
	errcodeAgentNotPermitted = 301002
)

// providerEnvelope is the errcode and errmsg pair that every WeCom response carries.
type providerEnvelope struct {
	Errcode *int   `json:"errcode"`
	Errmsg  string `json:"errmsg"`
}

// decodeProviderEnvelope requires a JSON object with a numeric errcode; WeCom reports errors with HTTP 200.
func decodeProviderEnvelope(body []byte, target any) (int, bool) {
	var envelope providerEnvelope
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Errcode == nil {
		return 0, false
	}
	if target != nil {
		if err := json.Unmarshal(body, target); err != nil {
			return 0, false
		}
	}
	return *envelope.Errcode, true
}

// isRetryableErrcode reports the codes WeCom returns without processing the call.
func isRetryableErrcode(errcode int) bool {
	return errcode == errcodeSystemBusy || errcode == errcodeFrequencyLimited || errcode == errcodeConcurrencyLimited
}

// failureKindForErrcode maps a definite WeCom refusal to a safe failure kind.
func failureKindForErrcode(errcode int) sdkgo.FailureKind {
	switch errcode {
	case errcodeInvalidSecret, errcodeInvalidCorpID, errcodeInvalidAccessToken, errcodeExpiredAccessToken, errcodeMissingAccessToken:
		return sdkgo.FailureAuthentication
	case errcodeUntrustedIP, errcodeNoPermission, errcodeAgentNotPermitted:
		return sdkgo.FailureAuthorization
	case errcodeFrequencyLimited, errcodeConcurrencyLimited:
		return sdkgo.FailureRateLimit
	case errcodeSystemBusy:
		return sdkgo.FailureAvailability
	}
	return sdkgo.FailureProviderRejection
}

// rejectionMessage names the errcode without echoing provider text, which can contain request values.
func rejectionMessage(subject string, errcode int) string {
	message := subject + " with errcode " + strconv.Itoa(errcode)
	if errcode == errcodeUntrustedIP {
		message += "; add the Dex Worker's public egress IP to the application's 企业可信IP (Trusted Company IP)"
	}
	return message
}

func newFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: providerName, Operation: operation, Message: message}
}
