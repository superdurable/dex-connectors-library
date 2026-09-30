// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package stripe connects Dex applications to Stripe-hosted Checkout Sessions.
package stripe

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/webhooktrigger"
)

const (
	maximumMetadataFields   = 50
	maximumMetadataKeyRunes = 40
	maximumMetadataValRunes = 500
)

var currencyPattern = regexp.MustCompile(`^[a-z]{3}$`)

// Option configures Client construction.
type Option func(*clientOptions)

type clientOptions struct {
	httpClient *http.Client
	now        func() time.Time
}

// WithHTTPClient replaces the HTTP client used for Stripe API calls.
func WithHTTPClient(client *http.Client) Option {
	return func(options *clientOptions) { options.httpClient = client }
}

// WithClock replaces the clock used for provider receipts and webhook validation.
func WithClock(now func() time.Time) Option {
	return func(options *clientOptions) { options.now = now }
}

// Client executes authenticated Stripe API calls and receives signed Stripe webhooks.
type Client struct {
	endpoint            *url.URL
	httpClient          *http.Client
	credentials         sdkgo.CredentialProvider[Credentials]
	maxResponseBytes    int64
	webhookMaxBodyBytes int64
	webhookTolerance    time.Duration
	now                 func() time.Time

	checkoutSessionEndpointsMu sync.Mutex
	checkoutSessionEndpoints   map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, CheckoutSessionEvent]
}

// CheckoutSession is the connector-safe subset of a Stripe Checkout Session.
type CheckoutSession struct {
	// ID is Stripe's Checkout Session identifier.
	ID string `json:"id"`
	// URL is Stripe's hosted Checkout URL while the session is open.
	URL string `json:"url,omitempty"`
	// ClientReferenceID is the non-sensitive application reference supplied at creation.
	ClientReferenceID string `json:"clientReferenceId,omitempty"`
	// PaymentStatus is Stripe's payment status, such as unpaid or paid.
	PaymentStatus string `json:"paymentStatus,omitempty"`
	// Status is Stripe's Checkout Session status, such as open, complete, or expired.
	Status string `json:"status,omitempty"`
	// PaymentIntentID is the safe Stripe PaymentIntent correlation identifier when present.
	PaymentIntentID string `json:"paymentIntentId,omitempty"`
	// Currency is the lowercase three-letter settlement currency.
	Currency string `json:"currency,omitempty"`
	// AmountTotal is the total amount in the currency's minor unit.
	AmountTotal int64 `json:"amountTotal,omitempty"`
	// ExpiresAt is the Checkout Session expiration time when present.
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
	// Metadata contains the bounded non-sensitive application metadata supplied at creation.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// CreateACHCheckoutSessionInput contains one Stripe-hosted ACH Checkout request.
type CreateACHCheckoutSessionInput struct {
	// ClientReferenceID correlates the session with one non-sensitive application record.
	ClientReferenceID string `json:"clientReferenceId"`
	// CustomerEmail is the plain email address Stripe pre-fills in hosted Checkout.
	CustomerEmail string `json:"customerEmail"`
	// SuccessURL is the HTTPS location Stripe uses after Checkout succeeds.
	SuccessURL string `json:"successUrl"`
	// CancelURL is the HTTPS location Stripe uses when the customer cancels Checkout.
	CancelURL string `json:"cancelUrl"`
	// Currency is the lowercase three-letter currency code.
	Currency string `json:"currency"`
	// UnitAmount is the positive ticket price in the currency's minor unit.
	UnitAmount int64 `json:"unitAmount"`
	// ProductName is the ticket or event label displayed by hosted Checkout.
	ProductName string `json:"productName"`
	// Metadata is bounded non-sensitive application metadata copied to the session and PaymentIntent.
	Metadata map[string]string `json:"metadata,omitempty"`
	// ExpiresAt optionally requests a provider expiration timestamp.
	ExpiresAt time.Time `json:"expiresAt,omitempty"`
}

// GetCheckoutSessionInput identifies one Stripe Checkout Session.
type GetCheckoutSessionInput struct {
	// SessionID is Stripe's Checkout Session identifier.
	SessionID string `json:"sessionId"`
}

// CreateACHCheckoutSessionOperation implements the Checkout Session mutation.
type CreateACHCheckoutSessionOperation struct{ client *Client }

// GetCheckoutSessionOperation implements the Checkout Session query.
type GetCheckoutSessionOperation struct{ client *Client }

type stripeCheckoutSession struct {
	ID                string            `json:"id"`
	Object            string            `json:"object"`
	URL               string            `json:"url"`
	ClientReferenceID string            `json:"client_reference_id"`
	PaymentStatus     string            `json:"payment_status"`
	Status            string            `json:"status"`
	PaymentIntent     flexibleObjectID  `json:"payment_intent"`
	Currency          string            `json:"currency"`
	AmountTotal       int64             `json:"amount_total"`
	ExpiresAt         int64             `json:"expires_at"`
	Metadata          map[string]string `json:"metadata"`
}

type stripeErrorResponse struct {
	Error struct {
		Type string `json:"type"`
		Code string `json:"code"`
	} `json:"error"`
}

type providerResponse struct {
	statusCode int
	header     http.Header
	body       []byte
}

var (
	errStripeRequestInvalid   = errors.New("Stripe request is invalid")
	errStripeResponseInvalid  = errors.New("Stripe response is invalid")
	errStripeResponseTooLarge = errors.New("Stripe response exceeds configured size limit")
)

// New validates configuration and constructs an authenticated Stripe client.
func New(config Config, credentials sdkgo.CredentialProvider[Credentials], options ...Option) (*Client, error) {
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	endpoint, err := url.Parse(config.Endpoint)
	if err != nil || endpoint.Scheme == "" || endpoint.Hostname() == "" {
		return nil, fmt.Errorf("Stripe endpoint must be absolute")
	}
	if endpoint.Scheme != "https" && endpoint.Hostname() != "localhost" && endpoint.Hostname() != "127.0.0.1" {
		return nil, fmt.Errorf("Stripe endpoint must use HTTPS")
	}
	if credentials == nil {
		return nil, fmt.Errorf("credential provider is required")
	}
	dependencies := clientOptions{now: time.Now}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("Stripe connector option is nil")
		}
		option(&dependencies)
	}
	if dependencies.httpClient == nil {
		dependencies.httpClient = &http.Client{Timeout: 25 * time.Second}
	}
	if dependencies.now == nil {
		return nil, fmt.Errorf("Stripe connector clock is required")
	}
	httpClient := *dependencies.httpClient
	httpClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	return &Client{
		endpoint: endpoint, httpClient: &httpClient, credentials: credentials,
		maxResponseBytes: config.MaxResponseBytes, webhookMaxBodyBytes: config.WebhookMaxBodyBytes,
		webhookTolerance: config.WebhookSignatureTolerance, now: dependencies.now,
		checkoutSessionEndpoints: make(map[sdkgo.ConnectionRef]*webhooktrigger.Endpoint[Credentials, CheckoutSessionEvent]),
	}, nil
}

// CreateACHCheckoutSession returns the Checkout Session mutation bound to this client.
func (client *Client) CreateACHCheckoutSession() CreateACHCheckoutSessionOperation {
	return CreateACHCheckoutSessionOperation{client: client}
}

// GetCheckoutSession returns the Checkout Session query bound to this client.
func (client *Client) GetCheckoutSession() GetCheckoutSessionOperation {
	return GetCheckoutSessionOperation{client: client}
}

// Definition returns the immutable connector operation definition.
func (CreateACHCheckoutSessionOperation) Definition() sdkgo.MutationDefinition {
	return CreateACHCheckoutSessionDefinition
}

// IdempotencyKey derives Stripe's provider key from the stable connector call ID.
func (CreateACHCheckoutSessionOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateACHCheckoutSessionInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one hosted Checkout Session that accepts US bank account payments.
func (operation CreateACHCheckoutSessionOperation) Invoke(call sdkgo.Call, input CreateACHCheckoutSessionInput) sdkgo.MutationAttempt[CheckoutSession] {
	if err := input.validate(); err != nil {
		return sdkgo.NewMutationBranch(CreateACHCheckoutSessionBranchDefect, CheckoutSession{}, stripeFailurePointer("createACHCheckoutSession", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, "createACHCheckoutSession")
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateACHCheckoutSessionBranchDefect, CheckoutSession{}, failure, sdkgo.Receipt{})
	}
	values := input.formValues()
	response, err := operation.client.request(call, credentials.SecretKey.Reveal(), http.MethodPost, "checkout/sessions", values)
	if err != nil {
		if errors.Is(err, errStripeRequestInvalid) {
			return sdkgo.NewMutationBranch(CreateACHCheckoutSessionBranchDefect, CheckoutSession{}, stripeFailurePointer("createACHCheckoutSession", sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
		}
		if errors.Is(err, errStripeResponseTooLarge) {
			return sdkgo.NewMutationBranch(CreateACHCheckoutSessionBranchInvalidResponse, CheckoutSession{}, stripeFailurePointer("createACHCheckoutSession", sdkgo.FailureResponseTooLarge, err.Error()), operation.client.receipt(call, response, ""))
		}
		return sdkgo.NewMutationUncertain(CheckoutSession{}, stripeFailure("createACHCheckoutSession", sdkgo.FailureTransport, "Stripe Checkout Session outcome is unknown"), operation.client.receipt(call, response, ""))
	}
	if response.statusCode == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[CheckoutSession](stripeFailure("createACHCheckoutSession", sdkgo.FailureRateLimit, "Stripe rate limited the Checkout Session request"), retryAfter(response.header))
	}
	if response.statusCode >= 500 {
		return sdkgo.NewMutationUncertain(CheckoutSession{}, stripeFailure("createACHCheckoutSession", sdkgo.FailureAvailability, "Stripe Checkout Session outcome is unknown"), operation.client.receipt(call, response, ""))
	}
	if response.statusCode < 200 || response.statusCode >= 300 {
		return sdkgo.NewMutationBranch(CreateACHCheckoutSessionBranchProviderRejected, CheckoutSession{}, classifyStripeRejection("createACHCheckoutSession", response), operation.client.receipt(call, response, ""))
	}
	session, err := decodeCheckoutSession(response.body)
	if err != nil {
		kind := sdkgo.FailureProtocol
		if errors.Is(err, errStripeResponseTooLarge) {
			kind = sdkgo.FailureResponseTooLarge
		}
		return sdkgo.NewMutationBranch(CreateACHCheckoutSessionBranchInvalidResponse, CheckoutSession{}, stripeFailurePointer("createACHCheckoutSession", kind, err.Error()), operation.client.receipt(call, response, ""))
	}
	return sdkgo.NewMutationBranch(CreateACHCheckoutSessionBranchCreated, session, nil, operation.client.receipt(call, response, session.ID))
}

// Definition returns the immutable connector operation definition.
func (GetCheckoutSessionOperation) Definition() sdkgo.QueryDefinition {
	return GetCheckoutSessionDefinition
}

// Invoke retrieves one Checkout Session without exposing customer bank details.
func (operation GetCheckoutSessionOperation) Invoke(call sdkgo.Call, input GetCheckoutSessionInput) sdkgo.QueryAttempt[CheckoutSession] {
	sessionID := strings.TrimSpace(input.SessionID)
	if sessionID == "" || !strings.HasPrefix(sessionID, "cs_") || strings.ContainsAny(sessionID, "/?#") {
		return sdkgo.NewQueryBranch(GetCheckoutSessionBranchDefect, CheckoutSession{}, stripeFailurePointer("getCheckoutSession", sdkgo.FailureValidation, "Stripe Checkout Session ID is required"), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, "getCheckoutSession")
	if failure != nil {
		return sdkgo.NewQueryBranch(GetCheckoutSessionBranchDefect, CheckoutSession{}, failure, sdkgo.Receipt{})
	}
	response, err := operation.client.request(call, credentials.SecretKey.Reveal(), http.MethodGet, "checkout/sessions/"+url.PathEscape(sessionID), nil)
	if err != nil {
		if errors.Is(err, errStripeResponseTooLarge) {
			return sdkgo.NewQueryBranch(GetCheckoutSessionBranchInvalidResponse, CheckoutSession{}, stripeFailurePointer("getCheckoutSession", sdkgo.FailureResponseTooLarge, err.Error()), operation.client.receipt(call, response, sessionID))
		}
		return sdkgo.NewQueryRetry[CheckoutSession](stripeFailure("getCheckoutSession", sdkgo.FailureAvailability, "Stripe Checkout Session is temporarily unavailable"), 0)
	}
	if response.statusCode == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[CheckoutSession](stripeFailure("getCheckoutSession", sdkgo.FailureRateLimit, "Stripe rate limited the Checkout Session query"), retryAfter(response.header))
	}
	if response.statusCode >= 500 {
		return sdkgo.NewQueryRetry[CheckoutSession](stripeFailure("getCheckoutSession", sdkgo.FailureAvailability, "Stripe Checkout Session is temporarily unavailable"), 0)
	}
	if response.statusCode == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetCheckoutSessionBranchNotFound, CheckoutSession{}, stripeFailurePointer("getCheckoutSession", sdkgo.FailureNotFound, "Stripe Checkout Session was not found"), operation.client.receipt(call, response, sessionID))
	}
	if response.statusCode < 200 || response.statusCode >= 300 {
		return sdkgo.NewQueryBranch(GetCheckoutSessionBranchProviderRejected, CheckoutSession{}, classifyStripeRejection("getCheckoutSession", response), operation.client.receipt(call, response, sessionID))
	}
	session, err := decodeCheckoutSession(response.body)
	if err != nil {
		kind := sdkgo.FailureProtocol
		if errors.Is(err, errStripeResponseTooLarge) {
			kind = sdkgo.FailureResponseTooLarge
		}
		return sdkgo.NewQueryBranch(GetCheckoutSessionBranchInvalidResponse, CheckoutSession{}, stripeFailurePointer("getCheckoutSession", kind, err.Error()), operation.client.receipt(call, response, sessionID))
	}
	return sdkgo.NewQueryBranch(GetCheckoutSessionBranchFound, session, nil, operation.client.receipt(call, response, session.ID))
}

func (input CreateACHCheckoutSessionInput) validate() error {
	if strings.TrimSpace(input.ClientReferenceID) == "" || len([]rune(input.ClientReferenceID)) > 200 {
		return fmt.Errorf("client reference ID must be non-empty and at most 200 characters")
	}
	address, err := mail.ParseAddress(strings.TrimSpace(input.CustomerEmail))
	if err != nil || address.Address != strings.TrimSpace(input.CustomerEmail) {
		return fmt.Errorf("customer email must be one plain email address")
	}
	if err := validateRedirectURL(input.SuccessURL, "success"); err != nil {
		return err
	}
	if err := validateRedirectURL(input.CancelURL, "cancel"); err != nil {
		return err
	}
	if !currencyPattern.MatchString(input.Currency) {
		return fmt.Errorf("currency must be a lowercase three-letter code")
	}
	if input.UnitAmount <= 0 || input.UnitAmount > 99999999 {
		return fmt.Errorf("unit amount must be from 1 through 99999999")
	}
	if strings.TrimSpace(input.ProductName) == "" || len([]rune(input.ProductName)) > 250 {
		return fmt.Errorf("product name must be non-empty and at most 250 characters")
	}
	if err := validateMetadata(input.Metadata); err != nil {
		return err
	}
	return nil
}

func validateRedirectURL(raw string, field string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Hostname() == "" || parsed.User != nil {
		return fmt.Errorf("%s URL must be absolute and must not contain credentials", field)
	}
	if parsed.Scheme != "https" && parsed.Hostname() != "localhost" && parsed.Hostname() != "127.0.0.1" {
		return fmt.Errorf("%s URL must use HTTPS", field)
	}
	return nil
}

func validateMetadata(metadata map[string]string) error {
	if len(metadata) > maximumMetadataFields {
		return fmt.Errorf("metadata cannot contain more than %d fields", maximumMetadataFields)
	}
	for key, value := range metadata {
		if strings.TrimSpace(key) == "" || len([]rune(key)) > maximumMetadataKeyRunes || strings.ContainsAny(key, "[]") {
			return fmt.Errorf("metadata keys must be non-empty, at most %d characters, and contain no square brackets", maximumMetadataKeyRunes)
		}
		if len([]rune(value)) > maximumMetadataValRunes {
			return fmt.Errorf("metadata values cannot exceed %d characters", maximumMetadataValRunes)
		}
	}
	return nil
}

func (input CreateACHCheckoutSessionInput) formValues() url.Values {
	values := url.Values{
		"mode":                                   {"payment"},
		"payment_method_types[0]":                {"us_bank_account"},
		"client_reference_id":                    {input.ClientReferenceID},
		"customer_email":                         {input.CustomerEmail},
		"success_url":                            {input.SuccessURL},
		"cancel_url":                             {input.CancelURL},
		"line_items[0][quantity]":                {"1"},
		"line_items[0][price_data][currency]":    {input.Currency},
		"line_items[0][price_data][unit_amount]": {strconv.FormatInt(input.UnitAmount, 10)},
		"line_items[0][price_data][product_data][name]": {input.ProductName},
	}
	for key, value := range input.Metadata {
		values.Set("metadata["+key+"]", value)
		values.Set("payment_intent_data[metadata]["+key+"]", value)
	}
	if !input.ExpiresAt.IsZero() {
		values.Set("expires_at", strconv.FormatInt(input.ExpiresAt.Unix(), 10))
	}
	return values
}

func (client *Client) request(call sdkgo.Call, secretKey string, method string, path string, values url.Values) (providerResponse, error) {
	target := strings.TrimRight(client.endpoint.String(), "/") + "/" + path
	var body io.Reader
	if method == http.MethodGet && len(values) > 0 {
		target += "?" + values.Encode()
	} else if values != nil {
		body = strings.NewReader(values.Encode())
	}
	request, err := http.NewRequestWithContext(call.Context, method, target, body)
	if err != nil {
		return providerResponse{}, fmt.Errorf("%w: request could not be built", errStripeRequestInvalid)
	}
	request.SetBasicAuth(secretKey, "")
	request.Header.Set("Accept", "application/json")
	if method == http.MethodPost {
		request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		request.Header.Set("Idempotency-Key", string(call.IdempotencyKey))
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{}, err
	}
	defer response.Body.Close()
	result := providerResponse{statusCode: response.StatusCode, header: response.Header.Clone()}
	result.body, err = io.ReadAll(io.LimitReader(response.Body, client.maxResponseBytes+1))
	if err != nil {
		return result, err
	}
	if int64(len(result.body)) > client.maxResponseBytes {
		return result, errStripeResponseTooLarge
	}
	return result, nil
}

func (client *Client) resolveCredentials(call sdkgo.Call, operation string) (Credentials, *sdkgo.Failure) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.SecretKey.Reveal() == "" {
		return Credentials{}, stripeFailurePointer(operation, sdkgo.FailureAuthentication, "Stripe connection credentials are unavailable")
	}
	return credentials, nil
}

func decodeCheckoutSession(contents []byte) (CheckoutSession, error) {
	if len(contents) == 0 {
		return CheckoutSession{}, fmt.Errorf("%w: Stripe returned an empty Checkout Session", errStripeResponseInvalid)
	}
	var decoded stripeCheckoutSession
	if err := jsonUnmarshal(contents, &decoded); err != nil {
		return CheckoutSession{}, fmt.Errorf("%w: Stripe returned malformed Checkout Session JSON", errStripeResponseInvalid)
	}
	if decoded.Object != "checkout.session" || decoded.ID == "" || !strings.HasPrefix(decoded.ID, "cs_") {
		return CheckoutSession{}, fmt.Errorf("%w: Stripe returned an invalid Checkout Session", errStripeResponseInvalid)
	}
	return decoded.normalized(), nil
}

func (session stripeCheckoutSession) normalized() CheckoutSession {
	normalized := CheckoutSession{
		ID: session.ID, URL: session.URL, ClientReferenceID: session.ClientReferenceID,
		PaymentStatus: session.PaymentStatus, Status: session.Status, PaymentIntentID: string(session.PaymentIntent),
		Currency: session.Currency, AmountTotal: session.AmountTotal, Metadata: session.Metadata,
	}
	if session.ExpiresAt > 0 {
		normalized.ExpiresAt = time.Unix(session.ExpiresAt, 0).UTC()
	}
	return normalized
}

func classifyStripeRejection(operation string, response providerResponse) *sdkgo.Failure {
	kind := sdkgo.FailureProviderRejection
	switch response.statusCode {
	case http.StatusUnauthorized:
		kind = sdkgo.FailureAuthentication
	case http.StatusForbidden:
		kind = sdkgo.FailureAuthorization
	case http.StatusNotFound:
		kind = sdkgo.FailureNotFound
	case http.StatusConflict:
		kind = sdkgo.FailureConflict
	}
	return stripeFailurePointer(operation, kind, "Stripe rejected the request")
}

func stripeFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "stripe", Operation: operation, Message: message}
}

func stripeFailurePointer(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	failure := stripeFailure(operation, kind, message)
	return &failure
}

func retryAfter(header http.Header) time.Duration {
	seconds, err := strconv.Atoi(header.Get("Retry-After"))
	if err != nil || seconds < 1 {
		return 0
	}
	return time.Duration(seconds) * time.Second
}

func (client *Client) receipt(call sdkgo.Call, response providerResponse, objectID string) sdkgo.Receipt {
	return sdkgo.Receipt{
		CallID: call.ID, IdempotencyKey: call.IdempotencyKey, Provider: "stripe", ProviderObjectID: objectID,
		ProviderRequestID: response.header.Get("Request-Id"), ObservedAt: client.now().UTC(),
	}
}
