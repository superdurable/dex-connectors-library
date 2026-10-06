// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package quickbooks connects Dex applications to the QuickBooks Online Accounting API.
//
// A Client exposes seven operations for an accounts-receivable process. FindCustomer and
// CreateCustomer resolve the customer, CreateInvoice, GetInvoice, ListInvoices, and SendInvoice
// raise, read, and email invoices, and RecordPayment applies a received payment to an invoice.
// Every amount is a Decimal, an exact base-10 string that never passes through floating point,
// and every value keeps QuickBooks's own vocabulary, such as EmailStatus NeedToSend.
//
// Every mutation sends the requestid query parameter derived from the Dex Call ID, so a retried
// or re-dispatched attempt of one Step execution receives QuickBooks's original response
// instead of creating a second customer, invoice, or payment, or sending a second email.
//
// A connection authorizes one QuickBooks company through Intuit's OAuth 2.0 consent. The
// connector reads the company's realm ID from the realmid claim of the ID token Intuit returns
// with the openid scope, or from the connection's realmId setting when the token names no
// company, and the connection's environment selects the production or sandbox API host. Access
// tokens last one hour and are refreshed with Intuit's rotating refresh token.
package quickbooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	// RealmIDReceiptKey names the Receipt metadata entry holding the QuickBooks company's realm ID.
	RealmIDReceiptKey = "realmId"
	// EnvironmentReceiptKey names the Receipt metadata entry holding the connection's environment.
	EnvironmentReceiptKey = "environment"
	// FaultCodeReceiptKey names the Receipt metadata entry holding the first QuickBooks Fault error code.
	FaultCodeReceiptKey = "faultCode"

	// MinorVersion is the QuickBooks Online API minor version every request sends.
	MinorVersion = "75"
	// DefaultInvoicesPerPage is the page size listInvoices uses when the input leaves it zero.
	DefaultInvoicesPerPage = 25
	// MaxInvoicesPerPage bounds one page so a Result stays small; QuickBooks itself allows up to 1000.
	MaxInvoicesPerPage = 100

	providerName      = "quickbooks"
	productionAPIHost = "quickbooks.api.intuit.com"
	sandboxAPIHost    = "sandbox-quickbooks.api.intuit.com"
	tokenHost         = "oauth.platform.intuit.com"

	// defaultRequestTimeout bounds one QuickBooks request inside the 30-second Execute timeout.
	defaultRequestTimeout = 20 * time.Second
	// maximumRequestIDBytes is the longest requestid QuickBooks accepts.
	maximumRequestIDBytes = 50

	intuitTransactionIDHeader = "intuit_tid"
)

var (
	errRequestNotBuilt = errors.New("QuickBooks request could not be built")
	// entityIDPattern matches a QuickBooks entity Id, a decimal string such as 145.
	entityIDPattern = regexp.MustCompile(`^[0-9]{1,20}$`)
	// transactionIDPattern bounds the intuit_tid header copied into a Receipt.
	transactionIDPattern = regexp.MustCompile(`^[A-Za-z0-9-]{1,64}$`)
)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient       *http.Client
	localProviderURL string
	now              func() time.Time
}

// WithHTTPClient overrides the default HTTP client, whose timeout is 20 seconds per request.
// The caller retains ownership of the client and its transport. The connector uses a copy that
// never follows redirects, so a token is never replayed to another host. The same client
// carries refresh requests to https://oauth.platform.intuit.com/oauth2/v1/tokens/bearer.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithLocalProviderURL sends every request for the QuickBooks production and sandbox API hosts
// and Intuit's token host to one local QuickBooks-compatible fake, keeping each request's path,
// for local verification only. The URL must be an http or https URL whose host is loopback,
// without a path, user information, a query, or a fragment, and the transport refuses every
// other host. Production connections leave it unset.
func WithLocalProviderURL(baseURL string) Option {
	return func(options *clientOptions) { options.localProviderURL = baseURL }
}

// WithClock overrides the clock used for Receipts, Retry-After dates, and token expiry.
// Tests use it; production connections leave it unset.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// Client executes authenticated QuickBooks Online Accounting API requests for connector
// operations. A Client is safe for concurrent use by several Steps.
type Client struct {
	httpClient        *http.Client
	credentials       CredentialSource
	refreshDriver     *CredentialRefreshDriver
	apiBaseURL        string
	environment       Environment
	configuredRealmID string
	maxResponseBytes  int64
	now               func() time.Time
}

// New validates configuration and constructs a QuickBooks client. Credentials are resolved
// before every provider call, so a refreshed token or a reauthorized company takes effect
// without a restart; the environment, realmId fallback, and response limit are startup
// configuration. Construction never contacts QuickBooks.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.MaxResponseBytes < 1 {
		return nil, errors.New("QuickBooks response limit must be positive")
	}
	configuredRealmID := strings.TrimSpace(config.RealmID)
	if configuredRealmID != "" && !realmIDPattern.MatchString(configuredRealmID) {
		return nil, errors.New("QuickBooks realmId must be the company ID, a number such as 9341453050298464")
	}
	if credentials == nil {
		return nil, errors.New("QuickBooks credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, errors.New("QuickBooks connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.now == nil {
		return nil, errors.New("QuickBooks connector clock is required")
	}
	callerClient := dependencies.httpClient
	if dependencies.localProviderURL != "" {
		routed, err := newLocalProviderHTTPClient(callerClient, dependencies.localProviderURL)
		if err != nil {
			return nil, err
		}
		callerClient = routed
	}
	httpClient := providerhttp.NewProviderHTTPClient(callerClient, defaultRequestTimeout)
	refreshDriver := newCredentialRefreshDriver(httpClient, dependencies.now)
	apiHost := productionAPIHost
	if config.Environment == EnvironmentSandbox {
		apiHost = sandboxAPIHost
	}
	return &Client{
		httpClient: httpClient, credentials: credentials, refreshDriver: refreshDriver,
		apiBaseURL: "https://" + apiHost + "/v3/company/", environment: config.Environment, configuredRealmID: configuredRealmID,
		maxResponseBytes: config.MaxResponseBytes, now: dependencies.now,
	}, nil
}

// FindCustomer returns the findCustomer Query bound to this client.
func (client *Client) FindCustomer() FindCustomerOperation {
	return FindCustomerOperation{client: client}
}

// CreateCustomer returns the createCustomer Mutation bound to this client.
func (client *Client) CreateCustomer() CreateCustomerOperation {
	return CreateCustomerOperation{client: client}
}

// CreateInvoice returns the createInvoice Mutation bound to this client.
func (client *Client) CreateInvoice() CreateInvoiceOperation {
	return CreateInvoiceOperation{client: client}
}

// GetInvoice returns the getInvoice Query bound to this client.
func (client *Client) GetInvoice() GetInvoiceOperation { return GetInvoiceOperation{client: client} }

// ListInvoices returns the listInvoices Query bound to this client.
func (client *Client) ListInvoices() ListInvoicesOperation {
	return ListInvoicesOperation{client: client}
}

// SendInvoice returns the sendInvoice Mutation bound to this client.
func (client *Client) SendInvoice() SendInvoiceOperation { return SendInvoiceOperation{client: client} }

// RecordPayment returns the recordPayment Mutation bound to this client.
func (client *Client) RecordPayment() RecordPaymentOperation {
	return RecordPaymentOperation{client: client}
}

// exchange sends one request and resends once after a 401, which QuickBooks answers before running anything.
func (client *Client) exchange(call sdkgo.Call, operation string, request quickbooksRequest) quickbooksExchange {
	credentials, realmID, failedResolution := client.resolveCredentials(call, operation)
	if failedResolution != nil {
		return *failedResolution
	}
	result := client.send(call, credentials, realmID, operation, request)
	if result.response.statusCode == http.StatusUnauthorized && client.canRefreshAfterRejection() {
		replacement, err := sdkgo.ResolveCredentialAfterRejection(call.Context, client.credentials, call, client.refreshDriver)
		// A refused or failed refresh keeps the 401; projects refresh after a rejection only once recorded expiry passed.
		if err == nil && validateResolvedCredentials(replacement) == nil {
			if replacementRealmID, realmErr := resolveRealmID(replacement, client.configuredRealmID); realmErr == nil && replacementRealmID == realmID {
				result = client.send(call, replacement, realmID, operation, request)
			}
		}
	}
	result.metadata = withMetadata(result.metadata, RealmIDReceiptKey, realmID)
	result.metadata = withMetadata(result.metadata, EnvironmentReceiptKey, string(client.environment))
	return result
}

// resolveCredentials separates a revoked grant from a transient token failure and a broken connection.
func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, string, *quickbooksExchange) {
	credentials, err := sdkgo.ResolveCredential(call.Context, client.credentials, call, client.refreshDriver)
	switch {
	case sdkgo.IsReauthorizationRequired(err):
		return Credentials{}, "", &quickbooksExchange{outcome: exchangeRejected,
			failure: quickbooksFailure(sdkgo.FailureAuthentication, operation, "QuickBooks authorization must be renewed: connect the company again")}
	case errors.Is(err, errCredentialRefreshUnavailable):
		return Credentials{}, "", &quickbooksExchange{outcome: exchangeRetry,
			failure: quickbooksFailure(sdkgo.FailureAvailability, operation, "Intuit access token refresh is temporarily unavailable")}
	case err != nil || validateResolvedCredentials(credentials) != nil:
		return Credentials{}, "", &quickbooksExchange{outcome: exchangeDefect,
			failure: quickbooksFailure(sdkgo.FailureAuthentication, operation, "QuickBooks connection credentials are unavailable")}
	}
	realmID, err := resolveRealmID(credentials, client.configuredRealmID)
	if err != nil {
		return Credentials{}, "", &quickbooksExchange{outcome: exchangeDefect,
			failure: quickbooksFailure(sdkgo.FailureAuthentication, operation, err.Error())}
	}
	return credentials, realmID, nil
}

func (client *Client) canRefreshAfterRejection() bool {
	_, supportsRejectionRefresh := client.credentials.(sdkgo.RejectedCredentialRefreshingProvider[Credentials])
	return supportsRejectionRefresh
}

// send performs one HTTP exchange and classifies it without deciding retry policy.
func (client *Client) send(call sdkgo.Call, credentials Credentials, realmID string, operation string, request quickbooksRequest) quickbooksExchange {
	httpRequest, err := client.buildRequest(call, credentials, realmID, request)
	if err != nil {
		return quickbooksExchange{outcome: exchangeDefect, failure: quickbooksFailure(sdkgo.FailureLocalDefect, operation, err.Error())}
	}
	httpResponse, err := client.httpClient.Do(httpRequest)
	if err != nil {
		if isConnectionNeverEstablished(err) {
			return quickbooksExchange{outcome: exchangeRetry, failure: quickbooksFailure(sdkgo.FailureTransport, operation, "QuickBooks could not be reached; no request was sent")}
		}
		return quickbooksExchange{outcome: exchangeRetry, failure: quickbooksFailure(sdkgo.FailureTransport, operation, "QuickBooks request failed before a response arrived")}
	}
	return classifyHTTPResponse(operation, httpResponse, credentials.AccessToken.Reveal(), client.maxResponseBytes, client.now())
}

// buildRequest addresses the company's realm, pins the minor version, and adds a mutation's requestid.
func (client *Client) buildRequest(call sdkgo.Call, credentials Credentials, realmID string, request quickbooksRequest) (*http.Request, error) {
	query := url.Values{}
	for name, values := range request.query {
		query[name] = values
	}
	query.Set("minorversion", MinorVersion)
	if request.isMutation {
		if call.IdempotencyKey == "" || len(call.IdempotencyKey) > maximumRequestIDBytes {
			return nil, errors.New("QuickBooks mutation has no usable requestid")
		}
		query.Set("requestid", string(call.IdempotencyKey))
	}
	var body io.Reader
	contentType := ""
	switch {
	case request.payload != nil:
		encoded, err := json.Marshal(request.payload)
		if err != nil {
			return nil, errRequestNotBuilt
		}
		body, contentType = bytes.NewReader(encoded), "application/json"
	case request.method == http.MethodPost:
		body, contentType = http.NoBody, "application/octet-stream"
	}
	target := client.apiBaseURL + realmID + request.path + "?" + query.Encode()
	httpRequest, err := http.NewRequestWithContext(call.Context, request.method, target, body)
	if err != nil {
		return nil, errRequestNotBuilt
	}
	httpRequest.Header.Set("Authorization", "Bearer "+credentials.AccessToken.Reveal())
	httpRequest.Header.Set("Accept", "application/json")
	if contentType != "" {
		httpRequest.Header.Set("Content-Type", contentType)
	}
	return httpRequest, nil
}

func (client *Client) receipt(call sdkgo.Call, result quickbooksExchange, objectID string) sdkgo.Receipt {
	receipt := sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: providerName,
		ProviderObjectID: objectID, ProviderRequestID: result.response.transactionID, ObservedAt: client.now().UTC(),
	}
	if len(result.metadata) != 0 {
		receipt.Metadata = result.metadata
	}
	return receipt
}
