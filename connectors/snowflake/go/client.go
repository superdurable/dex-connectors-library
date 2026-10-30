// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package snowflake connects Dex applications to Snowflake through the Snowflake SQL API at
// https://<account_identifier>.snowflakecomputing.com/api/v2/statements.
//
// A warehouse statement can run for minutes or hours, so the connector never holds a Dex Execute
// open while it runs. It exposes three operations that a Flow composes around a durable Timer:
//
//   - SubmitStatement sends one parameterized statement with async=true and returns its statement
//     handle. Every attempt of one Step execution sends the same requestId with retry=true, which
//     Snowflake documents as not running a statement again once it executed successfully.
//   - GetStatementResult reads the statement's status. It selects running while Snowflake is still
//     working and, once the statement finished, returns its column metadata and the bounded rows of
//     one result partition.
//   - CancelStatement asks Snowflake to cancel a statement that is still queued or running.
//
// The runnable example in examples/account-usage-summary submits a long aggregate, waits on a
// durable Timer between status reads, and cancels the statement when its wait budget runs out.
//
// Values are bound only through ? placeholders and typed SQL API bindings; the connector never
// formats a value into SQL text. Each connection authenticates with key-pair JWTs signed for every
// request or with a programmatic access token; both are resolved from the credential provider for
// every call, so a rotated key takes effect on the next Step.
package snowflake

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// KeyPairAuthMethodID identifies key-pair authentication with a JWT signed for every request.
	KeyPairAuthMethodID = "key-pair"
	// ProgrammaticAccessTokenAuthMethodID identifies authentication with a programmatic access token.
	ProgrammaticAccessTokenAuthMethodID = "programmatic-access-token"

	snowflakeDomain = "snowflakecomputing.com"
	statementsPath  = "/api/v2/statements"
	userAgent       = "dex-snowflake-connector/1.0"
	// requestTimeout bounds one SQL API request inside the 30-second Execute timeout.
	requestTimeout = 20 * time.Second
	// maximumStatementTimeoutSeconds is the SQL API's largest timeout field.
	maximumStatementTimeoutSeconds = 604800
	// maximumConfiguredRows bounds the maxRows connection setting.
	maximumConfiguredRows = 100000
	// maximumConfiguredResponseBytes bounds the maxResponseBytes connection setting.
	maximumConfiguredResponseBytes = 16 << 20
	// maximumContextIdentifierBytes bounds the warehouse, role, database, and schema settings.
	maximumContextIdentifierBytes = 255
)

var (
	// organizationAccountPattern is orgname-account_name; an account name never contains a hyphen.
	organizationAccountPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*-[A-Za-z][A-Za-z0-9_]*$`)
	// accountLocatorPattern is a locator with optional region and cloud segments, such as xy12345.us-east-2.aws.
	accountLocatorPattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*(\.[A-Za-z0-9]+(-[A-Za-z0-9]+)*){0,2}$`)
	statementHandlePattern = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
)

// Option supplies a non-serializable Client dependency.
type Option interface{ applyClientOption(*clientOptions) }

type clientOptions struct {
	httpClient       *http.Client
	localProviderURL string
	now              func() time.Time
}

type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(options *clientOptions) {
	options.httpClient = option.client
}

type localProviderURLOption struct{ baseURL string }

func (option localProviderURLOption) applyClientOption(options *clientOptions) {
	options.localProviderURL = option.baseURL
}

type clockOption struct{ now func() time.Time }

func (option clockOption) applyClientOption(options *clientOptions) { options.now = option.now }

// WithHTTPClient overrides the HTTP client, whose default timeout is 20 seconds per request.
// The caller keeps ownership of the client and its transport. The connector uses a copy that
// never follows redirects, so a credential is never replayed to another host.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client: client} }

// WithLocalProviderURL sends every SQL API request to one local Snowflake-compatible fake instead
// of https://<accountIdentifier>.snowflakecomputing.com, keeping each request's path and query,
// for local verification only. The URL must be an http or https URL whose host is loopback,
// without a path, user information, a query, or a fragment. Production connections leave it unset.
func WithLocalProviderURL(baseURL string) Option { return localProviderURLOption{baseURL: baseURL} }

// WithClock replaces the clock used for JWT issue times and Receipt observation times.
func WithClock(now func() time.Time) Option { return clockOption{now: now} }

// Client calls the Snowflake SQL API for one configured account and statement context.
//
// A Client is immutable after New and safe for concurrent use. It stores only non-secret
// settings and resolves credentials from its CredentialSource before every request.
type Client struct {
	baseURL                 string
	jwtAccount              string
	warehouse               string
	role                    string
	database                string
	schema                  string
	statementTimeoutSeconds int64
	maxRows                 int
	maxResponseBytes        int
	httpClient              *http.Client
	credentials             CredentialSource
	now                     func() time.Time
}

// New validates configuration and constructs a Snowflake SQL API client.
//
// Zero configuration fields take their manifest defaults. New returns an error for an account
// identifier that is not an orgname-account_name or account locator form, a statementTimeoutSeconds
// outside 1 through 604800, a context identifier with control characters or
// quotes, maxRows or maxResponseBytes above their documented limits, a nil credential source, or an
// invalid option. It never contacts Snowflake.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if credentials == nil {
		return nil, fmt.Errorf("snowflake credential source is required")
	}
	host, jwtAccount, err := resolveAccountHost(config.AccountIdentifier)
	if err != nil {
		return nil, err
	}
	if config.StatementTimeoutSeconds < 1 || config.StatementTimeoutSeconds > maximumStatementTimeoutSeconds {
		return nil, fmt.Errorf("snowflake statementTimeoutSeconds must be from 1 through %d", maximumStatementTimeoutSeconds)
	}
	for field, value := range map[string]string{
		"warehouse": config.Warehouse, "role": config.Role, "database": config.Database, "schema": config.Schema,
	} {
		if err := validateContextIdentifier(field, value); err != nil {
			return nil, err
		}
	}
	if config.MaxRows > maximumConfiguredRows {
		return nil, fmt.Errorf("snowflake maxRows cannot exceed %d", maximumConfiguredRows)
	}
	if config.MaxResponseBytes > maximumConfiguredResponseBytes {
		return nil, fmt.Errorf("snowflake maxResponseBytes cannot exceed %d", maximumConfiguredResponseBytes)
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("snowflake connector option is nil")
		}
		option.applyClientOption(&dependencies)
	}
	if dependencies.now == nil {
		return nil, fmt.Errorf("snowflake connector clock is required")
	}
	baseURL := "https://" + host
	if dependencies.localProviderURL != "" {
		baseURL, err = validateLocalProviderURL(dependencies.localProviderURL)
		if err != nil {
			return nil, err
		}
	}
	return &Client{
		baseURL: baseURL, jwtAccount: jwtAccount,
		warehouse: config.Warehouse, role: config.Role, database: config.Database, schema: config.Schema,
		statementTimeoutSeconds: config.StatementTimeoutSeconds, maxRows: int(config.MaxRows), maxResponseBytes: int(config.MaxResponseBytes),
		httpClient:  providerhttp.NewProviderHTTPClient(dependencies.httpClient, requestTimeout),
		credentials: credentials, now: dependencies.now,
	}, nil
}

// SubmitStatement returns the asynchronous statement submission operation bound to this client.
func (client *Client) SubmitStatement() SubmitStatementOperation {
	return SubmitStatementOperation{client: client}
}

// GetStatementResult returns the statement status and result partition operation bound to this client.
func (client *Client) GetStatementResult() GetStatementResultOperation {
	return GetStatementResultOperation{client: client}
}

// CancelStatement returns the statement cancellation operation bound to this client.
func (client *Client) CancelStatement() CancelStatementOperation {
	return CancelStatementOperation{client: client}
}

// sqlAPIRequest is one SQL API request; target is a path with its query.
type sqlAPIRequest struct {
	method string
	target string
	body   []byte
}

// send authenticates and dispatches one request; the caller owns the response body.
func (client *Client) send(call sdkgo.Call, request sqlAPIRequest) (*http.Response, error) {
	authorization, err := client.resolveAuthorization(call)
	if err != nil {
		return nil, err
	}
	ctx := call.Context
	if ctx == nil {
		return nil, errCallContextMissing
	}
	var body io.Reader
	if request.body != nil {
		body = bytes.NewReader(request.body)
	}
	httpRequest, err := http.NewRequestWithContext(context.Context(ctx), request.method, client.baseURL+request.target, body)
	if err != nil {
		return nil, errRequestNotBuilt
	}
	httpRequest.Header.Set("Authorization", "Bearer "+authorization.token.Reveal())
	httpRequest.Header.Set("X-Snowflake-Authorization-Token-Type", authorization.tokenType)
	httpRequest.Header.Set("Accept", "application/json")
	httpRequest.Header.Set("User-Agent", userAgent)
	if request.body != nil {
		httpRequest.Header.Set("Content-Type", "application/json")
	}
	response, err := client.httpClient.Do(httpRequest)
	if err != nil {
		return nil, &transportError{}
	}
	return response, nil
}

func (client *Client) receipt(call sdkgo.Call, statementHandle string, status statementStatus) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: ConnectorID, ObservedAt: client.now().UTC(),
	}
	if statementHandlePattern.MatchString(statementHandle) {
		receipt.ProviderObjectID = statementHandle
	}
	metadata := map[string]string{}
	if isSnowflakeCode(status.Code) {
		metadata["code"] = status.Code
	}
	if isSQLState(status.SQLState) {
		metadata["sqlState"] = status.SQLState
	}
	if len(metadata) > 0 {
		receipt.Metadata = metadata
	}
	return receipt
}

// resolveAccountHost returns the API host and the upper-cased account segment Snowflake requires in JWT claims.
func resolveAccountHost(accountIdentifier string) (string, string, error) {
	identifier := accountIdentifier
	if strings.TrimSpace(identifier) != identifier || identifier == "" {
		return "", "", fmt.Errorf("snowflake accountIdentifier must not be blank or contain surrounding whitespace")
	}
	lowered := strings.ToLower(identifier)
	if strings.Contains(lowered, "://") || strings.ContainsAny(lowered, "/:@?#") || strings.Contains(lowered, snowflakeDomain) {
		return "", "", fmt.Errorf("snowflake accountIdentifier must be the identifier alone, such as myorg-myaccount, without https:// or .%s", snowflakeDomain)
	}
	account, isPrivateLink := strings.CutSuffix(lowered, ".privatelink")
	firstSegment, _, _ := strings.Cut(account, ".")
	switch {
	case organizationAccountPattern.MatchString(account):
	case !strings.Contains(firstSegment, "-") && accountLocatorPattern.MatchString(account):
	case strings.Count(firstSegment, "-") > 1 && firstSegment == account:
		return "", "", fmt.Errorf("snowflake accountIdentifier must keep the account name's underscores, as View account details shows it, such as myorg-my_account")
	default:
		return "", "", fmt.Errorf("snowflake accountIdentifier must be orgname-account_name, such as myorg-myaccount, or an account locator such as xy12345.us-east-2.aws")
	}
	host := account
	if isPrivateLink {
		host += ".privatelink"
	}
	host += "." + snowflakeDomain
	if len(host) > 253 {
		return "", "", fmt.Errorf("snowflake accountIdentifier is too long")
	}
	return host, strings.ToUpper(firstSegment), nil
}

// validateContextIdentifier rejects values the SQL API would misread; quoting and case are Snowflake's to resolve.
func validateContextIdentifier(field string, value string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) != value || len(value) > maximumContextIdentifierBytes ||
		strings.ContainsFunc(value, unicode.IsControl) || strings.ContainsRune(value, '"') {
		return fmt.Errorf("snowflake %s must be an object name of at most %d bytes without surrounding spaces, quotes, or control characters", field, maximumContextIdentifierBytes)
	}
	return nil
}

func validateLocalProviderURL(value string) (string, error) {
	baseURL, err := providerhttp.ValidateBaseURL(value)
	if err != nil {
		return "", fmt.Errorf("snowflake local provider URL: %w", err)
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Path != "" {
		return "", fmt.Errorf("snowflake local provider URL must not contain a path")
	}
	host := strings.ToLower(parsed.Hostname())
	if host != "localhost" && host != "127.0.0.1" && host != "::1" {
		return "", fmt.Errorf("snowflake local provider URL must use a loopback host")
	}
	return baseURL, nil
}
