// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

// Package shopify provides read-only Shopify order Query operations for Dex Flows.
package shopify

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// Option supplies a non-serializable client dependency.
type Option interface{ applyClientOption(*Client) }
type httpClientOption struct{ client *http.Client }

func (option httpClientOption) applyClientOption(client *Client) { client.httpClient = option.client }

// WithHTTPClient supplies a transport; New copies it and disables redirects.
func WithHTTPClient(client *http.Client) Option { return httpClientOption{client} }

// Client holds immutable HTTP configuration and resolves credentials for each request.
type Client struct {
	baseURL          string
	maxResponseBytes int64
	credentials      CredentialSource
	httpClient       *http.Client
}

// New validates the connection settings without contacting Shopify.
func New(config Config, credentials CredentialSource, options ...Option) (*Client, error) {
	if credentials == nil {
		return nil, fmt.Errorf("Shopify credential source is required")
	}
	config = withConfigDefaults(config)
	if err := config.Validate(); err != nil {
		return nil, err
	}
	if config.Timeout <= 0 || config.MaxResponseBytes <= 0 {
		return nil, fmt.Errorf("timeout and maxResponseBytes must be positive")
	}
	if !isValidShopSubdomain(config.ShopSubdomain) {
		return nil, fmt.Errorf("shopSubdomain must contain lowercase letters, digits, or internal hyphens")
	}
	baseURL, err := providerhttp.ValidateBaseURL("https://" + config.ShopSubdomain + ".myshopify.com/admin/api/2026-10")
	if err != nil {
		return nil, err
	}
	client := &Client{baseURL: baseURL, maxResponseBytes: config.MaxResponseBytes, credentials: credentials}
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("client option is required")
		}
		option.applyClientOption(client)
	}
	client.httpClient = providerhttp.NewProviderHTTPClient(client.httpClient, config.Timeout)
	return client, nil
}

// GetOrderInput identifies one Shopify order.
type GetOrderInput struct {
	// OrderID is the numeric Shopify order ID.
	OrderID int64 `json:"orderId"`
}

// ListOrdersInput selects one bounded page of Shopify orders.
type ListOrdersInput struct {
	// Limit bounds one page from 1 through 250; zero uses Shopify default.
	Limit int `json:"limit,omitempty"`
	// PageInfo is the opaque cursor returned by a previous listOrders page.
	PageInfo string `json:"pageInfo,omitempty"`
	// Status filters open, closed, cancelled, or any orders; blank means any.
	Status string `json:"status,omitempty"`
	// FinancialStatus filters by Shopify's order financial status; blank omits the filter.
	FinancialStatus string `json:"financialStatus,omitempty"`
	// FulfillmentStatus filters by Shopify's order fulfillment status; blank omits the filter.
	FulfillmentStatus string `json:"fulfillmentStatus,omitempty"`
	// CreatedAtMin filters orders created on or after an RFC3339 timestamp.
	CreatedAtMin string `json:"createdAtMin,omitempty"`
	// CreatedAtMax filters orders created on or before an RFC3339 timestamp.
	CreatedAtMax string `json:"createdAtMax,omitempty"`
	// UpdatedAtMin filters orders updated on or after an RFC3339 timestamp.
	UpdatedAtMin string `json:"updatedAtMin,omitempty"`
	// UpdatedAtMax filters orders updated on or before an RFC3339 timestamp.
	UpdatedAtMax string `json:"updatedAtMax,omitempty"`
	// ProcessedAtMin filters orders processed on or after an RFC3339 timestamp.
	ProcessedAtMin string `json:"processedAtMin,omitempty"`
	// ProcessedAtMax filters orders processed on or before an RFC3339 timestamp.
	ProcessedAtMax string `json:"processedAtMax,omitempty"`
	// SinceID selects orders after a numeric Shopify order ID.
	SinceID int64 `json:"sinceId,omitempty"`
	// IDs selects a comma-separated list of numeric Shopify order IDs.
	IDs string `json:"ids,omitempty"`
	// AttributionAppID filters orders attributed to this numeric app ID or "current".
	AttributionAppID string `json:"attributionAppId,omitempty"`
}

// CountOrdersInput selects the order status to count.
type CountOrdersInput struct {
	// Status filters open, closed, cancelled, or any orders; blank means any.
	Status string `json:"status,omitempty"`
}

// OrderCount is Shopify's count of matching orders.
type OrderCount struct {
	// Count is the non-negative number of matching orders.
	Count int64 `json:"count"`
}

// Order is a minimal Shopify order summary; protected customer data is omitted.
type Order struct {
	// ID is the numeric Shopify order ID.
	ID int64 `json:"id"`
	// Name is the human-readable order number.
	Name string `json:"name"`
	// FinancialStatus is the payment state.
	FinancialStatus string `json:"financial_status"`
	// TotalPrice is the decimal order total.
	TotalPrice string `json:"total_price"`
	// Currency is the ISO currency code.
	Currency string `json:"currency"`
}

// OrderPage contains one Shopify order page.
type OrderPage struct {
	// Orders contains the returned orders, possibly none.
	Orders []Order `json:"orders"`
	// NextPageInfo is the opaque cursor for the next page; blank means no next page.
	NextPageInfo string `json:"nextPageInfo,omitempty"`
}

// GetOrderOperation executes the getOrder read.
type GetOrderOperation struct{ client *Client }

// GetOrder returns the getOrder Query bound to this client.
func (client *Client) GetOrder() GetOrderOperation { return GetOrderOperation{client: client} }

// Definition returns the manifest-generated getOrder contract.
func (GetOrderOperation) Definition() sdkgo.QueryDefinition { return GetOrderDefinition }

// Invoke reads the provider resource and classifies the response.
func (operation GetOrderOperation) Invoke(call sdkgo.Call, input GetOrderInput) sdkgo.QueryAttempt[Order] {
	query := url.Values{}
	if input.OrderID <= 0 {
		return failedAttempt[Order]("getOrder", GetOrderBranchDefect, sdkgo.FailureValidation, "orderId must be positive")
	}
	path := "/orders/" + strconv.FormatInt(input.OrderID, 10) + ".json"
	return executeRead(operation.client, call, "getOrder", path, query, GetOrderBranchFound, GetOrderBranchNotFound, GetOrderBranchProviderRejected, GetOrderBranchInvalidResponse, GetOrderBranchDefect, decodeGetOrderResponse)
}

func decodeGetOrderResponse(body []byte, _ http.Header) (Order, error) {
	var response struct {
		Order Order `json:"order"`
	}
	err := json.Unmarshal(body, &response)
	if err != nil || response.Order.ID <= 0 {
		return Order{}, fmt.Errorf("missing order")
	}
	return response.Order, nil
}

// ListOrdersOperation executes the listOrders read.
type ListOrdersOperation struct{ client *Client }

// ListOrders returns the listOrders Query bound to this client.
func (client *Client) ListOrders() ListOrdersOperation { return ListOrdersOperation{client: client} }

// Definition returns the manifest-generated listOrders contract.
func (ListOrdersOperation) Definition() sdkgo.QueryDefinition { return ListOrdersDefinition }

// Invoke reads the provider resource and classifies the response.
func (operation ListOrdersOperation) Invoke(call sdkgo.Call, input ListOrdersInput) sdkgo.QueryAttempt[OrderPage] {
	query := url.Values{}
	path := "/orders.json"
	if input.Limit < 0 || input.Limit > 250 {
		return failedAttempt[OrderPage]("listOrders", ListOrdersBranchDefect, sdkgo.FailureValidation, "limit must be 1 through 250, or zero for provider default")
	}
	if input.PageInfo != "" {
		if len(input.PageInfo) > 2048 || strings.ContainsAny(input.PageInfo, " \r\n") || input.Status != "" || input.FinancialStatus != "" || input.FulfillmentStatus != "" || input.CreatedAtMin != "" || input.CreatedAtMax != "" || input.UpdatedAtMin != "" || input.UpdatedAtMax != "" || input.ProcessedAtMin != "" || input.ProcessedAtMax != "" || input.SinceID != 0 || input.IDs != "" || input.AttributionAppID != "" {
			return failedAttempt[OrderPage]("listOrders", ListOrdersBranchDefect, sdkgo.FailureValidation, "pageInfo cannot be combined with filters and must be a bounded opaque cursor")
		}
		query.Set("page_info", input.PageInfo)
	} else {
		if !isValidOrderStatus(input.Status) || !isValidFinancialStatus(input.FinancialStatus) || !isValidFulfillmentStatus(input.FulfillmentStatus) || !isValidOrderTimestamps(input) || input.SinceID < 0 || !isValidNumericIDList(input.IDs) || !isValidAttributionAppID(input.AttributionAppID) {
			return failedAttempt[OrderPage]("listOrders", ListOrdersBranchDefect, sdkgo.FailureValidation, "order filters are invalid")
		}
		query.Set("status", orderStatusOrAny(input.Status))
		setOptionalOrderFilter(query, "financial_status", input.FinancialStatus)
		setOptionalOrderFilter(query, "fulfillment_status", input.FulfillmentStatus)
		setOptionalOrderFilter(query, "created_at_min", input.CreatedAtMin)
		setOptionalOrderFilter(query, "created_at_max", input.CreatedAtMax)
		setOptionalOrderFilter(query, "updated_at_min", input.UpdatedAtMin)
		setOptionalOrderFilter(query, "updated_at_max", input.UpdatedAtMax)
		setOptionalOrderFilter(query, "processed_at_min", input.ProcessedAtMin)
		setOptionalOrderFilter(query, "processed_at_max", input.ProcessedAtMax)
		setOptionalOrderFilter(query, "ids", input.IDs)
		setOptionalOrderFilter(query, "attribution_app_id", input.AttributionAppID)
		if input.SinceID > 0 {
			query.Set("since_id", strconv.FormatInt(input.SinceID, 10))
		}
	}
	if input.Limit > 0 {
		query.Set("limit", strconv.Itoa(input.Limit))
	}
	return executeRead(operation.client, call, "listOrders", path, query, ListOrdersBranchListed, ListOrdersBranchNotFound, ListOrdersBranchProviderRejected, ListOrdersBranchInvalidResponse, ListOrdersBranchDefect, decodeListOrdersResponse)
}

func decodeListOrdersResponse(body []byte, headers http.Header) (OrderPage, error) {
	var response OrderPage
	err := json.Unmarshal(body, &response)
	if err != nil {
		return OrderPage{}, err
	}
	if response.Orders == nil {
		return OrderPage{}, fmt.Errorf("missing orders")
	}
	response.NextPageInfo = nextPageInfo(headers.Get("Link"))
	return response, nil
}

// CountOrdersOperation executes the countOrders read.
type CountOrdersOperation struct{ client *Client }

// CountOrders returns the countOrders Query bound to this client.
func (client *Client) CountOrders() CountOrdersOperation { return CountOrdersOperation{client: client} }

// Definition returns the manifest-generated countOrders contract.
func (CountOrdersOperation) Definition() sdkgo.QueryDefinition { return CountOrdersDefinition }

// Invoke counts orders with an optional status filter.
func (operation CountOrdersOperation) Invoke(call sdkgo.Call, input CountOrdersInput) sdkgo.QueryAttempt[OrderCount] {
	if !isValidOrderStatus(input.Status) {
		return failedAttempt[OrderCount]("countOrders", CountOrdersBranchDefect, sdkgo.FailureValidation, "status must be open, closed, cancelled, or any")
	}
	query := url.Values{"status": {orderStatusOrAny(input.Status)}}
	return executeRead(operation.client, call, "countOrders", "/orders/count.json", query, CountOrdersBranchCounted, CountOrdersBranchNotFound, CountOrdersBranchProviderRejected, CountOrdersBranchInvalidResponse, CountOrdersBranchDefect, decodeCountOrdersResponse)
}

func decodeCountOrdersResponse(body []byte, _ http.Header) (OrderCount, error) {
	var response struct {
		Count *int64 `json:"count"`
	}
	if err := json.Unmarshal(body, &response); err != nil || response.Count == nil || *response.Count < 0 {
		return OrderCount{}, fmt.Errorf("missing or invalid count")
	}
	return OrderCount{Count: *response.Count}, nil
}

func isValidOrderStatus(value string) bool {
	switch value {
	case "", "any", "open", "closed", "cancelled":
		return true
	}
	return false
}

func orderStatusOrAny(value string) string {
	if value == "" {
		return "any"
	}
	return value
}

func isValidFinancialStatus(value string) bool {
	switch value {
	case "", "any", "authorized", "pending", "paid", "partially_paid", "refunded", "voided", "partially_refunded", "unpaid":
		return true
	}
	return false
}

func isValidFulfillmentStatus(value string) bool {
	switch value {
	case "", "any", "shipped", "partial", "unshipped", "unfulfilled":
		return true
	}
	return false
}

func isValidOrderTimestamps(input ListOrdersInput) bool {
	for _, value := range []string{input.CreatedAtMin, input.CreatedAtMax, input.UpdatedAtMin, input.UpdatedAtMax, input.ProcessedAtMin, input.ProcessedAtMax} {
		if value != "" {
			if _, err := time.Parse(time.RFC3339, value); err != nil {
				return false
			}
		}
	}
	return true
}

func isValidNumericIDList(value string) bool {
	if value == "" {
		return true
	}
	ids := strings.Split(value, ",")
	if len(ids) > 250 {
		return false
	}
	for _, id := range ids {
		parsed, err := strconv.ParseInt(id, 10, 64)
		if err != nil || parsed <= 0 {
			return false
		}
	}
	return true
}

func isValidAttributionAppID(value string) bool {
	if value == "" || value == "current" {
		return true
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	return err == nil && parsed > 0
}

func setOptionalOrderFilter(query url.Values, name, value string) {
	if value != "" {
		query.Set(name, value)
	}
}

func nextPageInfo(linkHeader string) string {
	for _, link := range strings.Split(linkHeader, ",") {
		if !strings.Contains(link, `rel="next"`) {
			continue
		}
		start, end := strings.Index(link, "<"), strings.Index(link, ">")
		if start < 0 || end <= start {
			continue
		}
		parsed, err := url.Parse(link[start+1 : end])
		if err == nil {
			return parsed.Query().Get("page_info")
		}
	}
	return ""
}

type authResult struct {
	failure    *sdkgo.Failure
	branch     sdkgo.BranchID
	retry      bool
	retryAfter time.Duration
}

func (client *Client) authorize(call sdkgo.Call, operation string, request *http.Request, defectBranch, rejectedBranch, invalidBranch sdkgo.BranchID) authResult {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil || !providerhttp.IsHeaderSafeCredential(credentials.AccessToken.Reveal()) {
		return authResult{failure: failureFor(operation, sdkgo.FailureAuthentication, "Shopify credentials are unavailable"), branch: defectBranch}
	}
	request.Header.Set("X-Shopify-Access-Token", credentials.AccessToken.Reveal())
	return authResult{}
}

func executeRead[T any](client *Client, call sdkgo.Call, operation, path string, query url.Values, successBranch, notFoundBranch, rejectedBranch, invalidBranch, defectBranch sdkgo.BranchID, decode func([]byte, http.Header) (T, error)) sdkgo.QueryAttempt[T] {
	endpoint := client.baseURL + path
	if len(query) > 0 {
		endpoint += "?" + query.Encode()
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodGet, endpoint, nil)
	if err != nil {
		return failedAttempt[T](operation, defectBranch, sdkgo.FailureLocalDefect, "provider request could not be built")
	}
	request.Header.Set("Accept", "application/json")
	authorization := client.authorize(call, operation, request, defectBranch, rejectedBranch, invalidBranch)
	if authorization.failure != nil {
		if authorization.retry {
			return sdkgo.NewQueryRetry[T](*authorization.failure, authorization.retryAfter)
		}
		return sdkgo.NewQueryBranch(authorization.branch, *new(T), authorization.failure, sdkgo.Receipt{})
	}
	response, err := client.httpClient.Do(request)
	if err != nil {
		return sdkgo.NewQueryRetry[T](*failureFor(operation, sdkgo.FailureAvailability, "provider read failed"), 0)
	}
	defer func() { _ = response.Body.Close() }()
	switch {
	case response.StatusCode == http.StatusTooManyRequests:
		return sdkgo.NewQueryRetry[T](*failureFor(operation, sdkgo.FailureRateLimit, "provider rate limited the read"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusRequestTimeout || response.StatusCode >= 500:
		return sdkgo.NewQueryRetry[T](*failureFor(operation, sdkgo.FailureAvailability, "provider read is temporarily unavailable"), providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()))
	case response.StatusCode == http.StatusNotFound:
		return failedAttempt[T](operation, notFoundBranch, sdkgo.FailureProviderRejection, "resource was not found")
	case response.StatusCode == http.StatusUnauthorized:
		return failedAttempt[T](operation, rejectedBranch, sdkgo.FailureAuthentication, "provider rejected credentials")
	case response.StatusCode == http.StatusForbidden:
		return failedAttempt[T](operation, rejectedBranch, sdkgo.FailureAuthorization, "provider denied access")
	case response.StatusCode != http.StatusOK:
		return failedAttempt[T](operation, rejectedBranch, sdkgo.FailureProviderRejection, "provider rejected read with HTTP "+strconv.Itoa(response.StatusCode))
	}
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	if errors.Is(err, providerhttp.ErrBodyTooLarge) {
		return failedAttempt[T](operation, invalidBranch, sdkgo.FailureResponseTooLarge, "provider response exceeded the configured limit")
	}
	if err != nil {
		return sdkgo.NewQueryRetry[T](*failureFor(operation, sdkgo.FailureAvailability, "provider response was interrupted"), 0)
	}
	value, err := decode(body, response.Header)
	if err != nil {
		return failedAttempt[T](operation, invalidBranch, sdkgo.FailureProtocol, "provider returned invalid JSON")
	}
	return sdkgo.NewQueryBranch(successBranch, value, nil, sdkgo.Receipt{})
}

func validPathSegment(value string) bool {
	return value != "" && value != "." && value != ".." && !strings.ContainsAny(value, "/?#") && strings.IndexFunc(value, func(character rune) bool { return character < 32 }) < 0
}

func isValidShopSubdomain(value string) bool {
	if len(value) == 0 || len(value) > 63 || value[0] == '-' || value[len(value)-1] == '-' {
		return false
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '-' {
			return false
		}
	}
	return true
}

func failedAttempt[T any](operation string, branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[T] {
	return sdkgo.NewQueryBranch(branch, *new(T), failureFor(operation, kind, message), sdkgo.Receipt{})
}
func failureFor(operation string, kind sdkgo.FailureKind, message string) *sdkgo.Failure {
	return &sdkgo.Failure{Kind: kind, Provider: ConnectorID, Operation: operation, Message: message}
}
