// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package razorpay

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

// PaymentLinkSummary contains shareable link details without customer information.
type PaymentLinkSummary struct {
	// ID is the Razorpay standard payment link ID.
	ID string `json:"id"`
	// Status is the current link state.
	Status string `json:"status"`
	// Amount is the requested amount in currency subunits.
	Amount int64 `json:"amount"`
	// CurrencyCode is the ISO 4217 currency code.
	CurrencyCode string `json:"currencyCode"`
	// ReferenceID identifies the merchant's link request when present.
	ReferenceID string `json:"referenceId"`
	// ShortURL is the provider-hosted payment URL to share with a payer.
	ShortURL string `json:"shortUrl"`
}

// ListPaymentLinksInput optionally narrows the list to one merchant reference.
type ListPaymentLinksInput struct {
	// ReferenceID is an exact reference ID; blank lists all links within the response limit.
	ReferenceID string `json:"referenceId"`
}

// PaymentLinkPage contains standard payment links returned by Razorpay.
type PaymentLinkPage struct {
	// Links contains only shareable link metadata.
	Links []PaymentLinkSummary `json:"links"`
}

// GetPaymentLinkInput selects a standard link by ID.
type GetPaymentLinkInput struct {
	// PaymentLinkID is the exact ID prefixed plink_.
	PaymentLinkID string `json:"paymentLinkId"`
}

// CreatePaymentLinkInput supplies a standard link amount and currency.
type CreatePaymentLinkInput struct {
	// Amount is the positive requested amount in currency subunits.
	Amount int64 `json:"amount"`
	// CurrencyCode is the uppercase ISO 4217 code.
	CurrencyCode string `json:"currencyCode"`
}

// CancelPaymentLinkInput selects an unpaid standard link to cancel.
type CancelPaymentLinkInput struct {
	// PaymentLinkID is the exact ID prefixed plink_.
	PaymentLinkID string `json:"paymentLinkId"`
}

// ListPaymentLinksOperation lists standard payment links.
type ListPaymentLinksOperation struct{ client *Client }

// ListPaymentLinks binds the Query to this client.
func (client *Client) ListPaymentLinks() ListPaymentLinksOperation {
	return ListPaymentLinksOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (ListPaymentLinksOperation) Definition() sdkgo.QueryDefinition {
	return ListPaymentLinksDefinition
}

// Invoke lists links, excluding customer, payment, and note details.
func (operation ListPaymentLinksOperation) Invoke(call sdkgo.Call, input ListPaymentLinksInput) sdkgo.QueryAttempt[PaymentLinkPage] {
	if len(input.ReferenceID) > 40 || strings.ContainsAny(input.ReferenceID, "\r\n") {
		return paymentLinkPageBranch(ListPaymentLinksBranchDefect, sdkgo.FailureValidation, "Razorpay link reference is invalid")
	}
	path := "/payment_links"
	if input.ReferenceID != "" {
		path += "?reference_id=" + url.QueryEscape(input.ReferenceID)
	}
	response := operation.client.get(call, path)
	if errors.Is(response.err, errCredential) {
		return paymentLinkPageBranch(ListPaymentLinksBranchDefect, sdkgo.FailureAuthentication, "Razorpay credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[PaymentLinkPage](paymentLinkFailure("listPaymentLinks", sdkgo.FailureTransport, "Razorpay link list request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[PaymentLinkPage](paymentLinkFailure("listPaymentLinks", sdkgo.FailureRateLimit, "Razorpay rate limited link listing"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[PaymentLinkPage](paymentLinkFailure("listPaymentLinks", sdkgo.FailureAvailability, "Razorpay link listing is unavailable"), response.retryAfter)
	}
	if response.status != http.StatusOK {
		return paymentLinkPageBranch(ListPaymentLinksBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected link listing")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return paymentLinkPageBranch(ListPaymentLinksBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Razorpay link list was oversized")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[PaymentLinkPage](paymentLinkFailure("listPaymentLinks", sdkgo.FailureTransport, "Razorpay link list was interrupted"), 0)
	}
	page, valid := decodePaymentLinkPage(response.body)
	if !valid {
		return paymentLinkPageBranch(ListPaymentLinksBranchInvalidResponse, sdkgo.FailureProtocol, "Razorpay link list is invalid")
	}
	return sdkgo.NewQueryBranch(ListPaymentLinksBranchListed, page, nil, sdkgo.Receipt{})
}

// GetPaymentLinkOperation reads one standard payment link.
type GetPaymentLinkOperation struct{ client *Client }

// GetPaymentLink binds the Query to this client.
func (client *Client) GetPaymentLink() GetPaymentLinkOperation {
	return GetPaymentLinkOperation{client}
}

// Definition returns the manifest-generated Query contract.
func (GetPaymentLinkOperation) Definition() sdkgo.QueryDefinition { return GetPaymentLinkDefinition }

// Invoke reads safe link fields by ID.
func (operation GetPaymentLinkOperation) Invoke(call sdkgo.Call, input GetPaymentLinkInput) sdkgo.QueryAttempt[PaymentLinkSummary] {
	if !paymentLinkIDPattern.MatchString(input.PaymentLinkID) {
		return getPaymentLinkBranch(GetPaymentLinkBranchDefect, sdkgo.FailureValidation, "Razorpay link ID is invalid")
	}
	response := operation.client.get(call, "/payment_links/"+url.PathEscape(input.PaymentLinkID))
	if errors.Is(response.err, errCredential) {
		return getPaymentLinkBranch(GetPaymentLinkBranchDefect, sdkgo.FailureAuthentication, "Razorpay credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return sdkgo.NewQueryRetry[PaymentLinkSummary](paymentLinkFailure("getPaymentLink", sdkgo.FailureTransport, "Razorpay link request failed"), 0)
	}
	if response.status == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[PaymentLinkSummary](paymentLinkFailure("getPaymentLink", sdkgo.FailureRateLimit, "Razorpay rate limited link lookup"), response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return sdkgo.NewQueryRetry[PaymentLinkSummary](paymentLinkFailure("getPaymentLink", sdkgo.FailureAvailability, "Razorpay link lookup is unavailable"), response.retryAfter)
	}
	if response.status == http.StatusNotFound {
		return getPaymentLinkBranch(GetPaymentLinkBranchNotFound, sdkgo.FailureNotFound, "Razorpay link was not found")
	}
	if response.status != http.StatusOK {
		return getPaymentLinkBranch(GetPaymentLinkBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected link lookup")
	}
	if errors.Is(response.err, providerhttp.ErrBodyTooLarge) {
		return getPaymentLinkBranch(GetPaymentLinkBranchInvalidResponse, sdkgo.FailureResponseTooLarge, "Razorpay link response was oversized")
	}
	if response.err != nil {
		return sdkgo.NewQueryRetry[PaymentLinkSummary](paymentLinkFailure("getPaymentLink", sdkgo.FailureTransport, "Razorpay link response was interrupted"), 0)
	}
	link, valid := decodePaymentLinkSummary(response.body)
	if !valid || link.ID != input.PaymentLinkID {
		return getPaymentLinkBranch(GetPaymentLinkBranchInvalidResponse, sdkgo.FailureProtocol, "Razorpay link response is invalid")
	}
	return sdkgo.NewQueryBranch(GetPaymentLinkBranchFound, link, nil, sdkgo.Receipt{})
}

// CreatePaymentLinkOperation creates a standard link with a unique reference.
type CreatePaymentLinkOperation struct{ client *Client }

// CreatePaymentLink binds the Mutation to this client.
func (client *Client) CreatePaymentLink() CreatePaymentLinkOperation {
	return CreatePaymentLinkOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (CreatePaymentLinkOperation) Definition() sdkgo.MutationDefinition {
	return CreatePaymentLinkDefinition
}

// IdempotencyKey uses the Dex Call ID to derive Razorpay's unique reference ID.
func (CreatePaymentLinkOperation) IdempotencyKey(callID sdkgo.CallID, _ CreatePaymentLinkInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates a link without notifications and confirms duplicate references by read-back.
func (operation CreatePaymentLinkOperation) Invoke(call sdkgo.Call, input CreatePaymentLinkInput) sdkgo.MutationAttempt[PaymentLinkSummary] {
	if input.Amount <= 0 || !validCurrencyCode(input.CurrencyCode) {
		return createPaymentLinkBranch(CreatePaymentLinkBranchDefect, sdkgo.FailureValidation, "Razorpay link amount or currency is invalid")
	}
	referenceID := "dex_" + strings.ReplaceAll(string(call.IdempotencyKey), "-", "")
	if len(referenceID) != 36 {
		return createPaymentLinkBranch(CreatePaymentLinkBranchDefect, sdkgo.FailureLocalDefect, "Razorpay link reference could not be derived")
	}
	payload, _ := json.Marshal(map[string]any{"amount": input.Amount, "currency": input.CurrencyCode, "reference_id": referenceID, "notify": map[string]bool{"sms": false, "email": false}})
	response := operation.client.postPaymentLink(call, "/payment_links", payload)
	if errors.Is(response.err, errCredential) {
		return createPaymentLinkBranch(CreatePaymentLinkBranchDefect, sdkgo.FailureAuthentication, "Razorpay credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return createPaymentLinkRetry(sdkgo.FailureTransport, "Razorpay link request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return createPaymentLinkRetry(sdkgo.FailureRateLimit, "Razorpay rate limited link creation", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return createPaymentLinkRetry(sdkgo.FailureAvailability, "Razorpay link outcome is unknown", response.retryAfter)
	}
	if response.status == http.StatusBadRequest && response.err == nil && isDuplicatePaymentLinkReference(response.body) {
		return operation.confirmCreatedLink(call, input, referenceID)
	}
	if response.status != http.StatusOK && response.status != http.StatusCreated {
		return createPaymentLinkBranch(CreatePaymentLinkBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected link creation")
	}
	if response.err != nil {
		return createPaymentLinkRetry(sdkgo.FailureTransport, "Razorpay link response was interrupted", 0)
	}
	link, valid := decodePaymentLinkSummary(response.body)
	if !valid || link.ReferenceID != referenceID || link.Amount != input.Amount || link.CurrencyCode != input.CurrencyCode {
		return createPaymentLinkRetry(sdkgo.FailureProtocol, "Razorpay link may exist but response was invalid", 0)
	}
	return sdkgo.NewMutationBranch(CreatePaymentLinkBranchCreated, link, nil, sdkgo.Receipt{})
}

func (operation CreatePaymentLinkOperation) confirmCreatedLink(call sdkgo.Call, input CreatePaymentLinkInput, referenceID string) sdkgo.MutationAttempt[PaymentLinkSummary] {
	response := operation.client.get(call, "/payment_links?reference_id="+url.QueryEscape(referenceID))
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return createPaymentLinkRetry(sdkgo.FailureAvailability, "Razorpay link reference could not be checked", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return createPaymentLinkBranch(CreatePaymentLinkBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected link reference lookup")
	}
	page, valid := decodePaymentLinkPage(response.body)
	if !valid {
		return createPaymentLinkRetry(sdkgo.FailureProtocol, "Razorpay link reference response was invalid", 0)
	}
	for _, link := range page.Links {
		if link.ReferenceID == referenceID && link.Amount == input.Amount && link.CurrencyCode == input.CurrencyCode {
			return sdkgo.NewMutationBranch(CreatePaymentLinkBranchCreated, link, nil, sdkgo.Receipt{})
		}
	}
	return createPaymentLinkRetry(sdkgo.FailureAvailability, "Razorpay link reference is not yet visible", 0)
}

// CancelPaymentLinkOperation cancels one unpaid standard link.
type CancelPaymentLinkOperation struct{ client *Client }

// CancelPaymentLink binds the Mutation to this client.
func (client *Client) CancelPaymentLink() CancelPaymentLinkOperation {
	return CancelPaymentLinkOperation{client}
}

// Definition returns the manifest-generated Mutation contract.
func (CancelPaymentLinkOperation) Definition() sdkgo.MutationDefinition {
	return CancelPaymentLinkDefinition
}

// IdempotencyKey records the call identity; cancellation is a one-way link state change.
func (CancelPaymentLinkOperation) IdempotencyKey(callID sdkgo.CallID, _ CancelPaymentLinkInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke cancels a link or confirms its cancelled state after a duplicate rejection.
func (operation CancelPaymentLinkOperation) Invoke(call sdkgo.Call, input CancelPaymentLinkInput) sdkgo.MutationAttempt[PaymentLinkSummary] {
	if !paymentLinkIDPattern.MatchString(input.PaymentLinkID) {
		return cancelPaymentLinkBranch(CancelPaymentLinkBranchDefect, sdkgo.FailureValidation, "Razorpay link ID is invalid")
	}
	response := operation.client.postPaymentLink(call, "/payment_links/"+url.PathEscape(input.PaymentLinkID)+"/cancel", nil)
	if errors.Is(response.err, errCredential) {
		return cancelPaymentLinkBranch(CancelPaymentLinkBranchDefect, sdkgo.FailureAuthentication, "Razorpay credentials are unavailable")
	}
	if errors.Is(response.err, errRequest) {
		return cancelPaymentLinkRetry(sdkgo.FailureTransport, "Razorpay cancel request failed", 0)
	}
	if response.status == http.StatusTooManyRequests {
		return cancelPaymentLinkRetry(sdkgo.FailureRateLimit, "Razorpay rate limited cancellation", response.retryAfter)
	}
	if response.status == http.StatusRequestTimeout || response.status >= 500 {
		return cancelPaymentLinkRetry(sdkgo.FailureAvailability, "Razorpay cancellation outcome is unknown", response.retryAfter)
	}
	if response.status == http.StatusBadRequest {
		return operation.confirmCancelledLink(call, input.PaymentLinkID)
	}
	if response.status != http.StatusOK {
		return cancelPaymentLinkBranch(CancelPaymentLinkBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected cancellation")
	}
	if response.err != nil {
		return cancelPaymentLinkRetry(sdkgo.FailureTransport, "Razorpay cancel response was interrupted", 0)
	}
	link, valid := decodePaymentLinkSummary(response.body)
	if !valid || link.ID != input.PaymentLinkID || link.Status != "cancelled" {
		return cancelPaymentLinkRetry(sdkgo.FailureProtocol, "Razorpay cancellation may have completed but response was invalid", 0)
	}
	return sdkgo.NewMutationBranch(CancelPaymentLinkBranchCancelled, link, nil, sdkgo.Receipt{})
}

func (operation CancelPaymentLinkOperation) confirmCancelledLink(call sdkgo.Call, linkID string) sdkgo.MutationAttempt[PaymentLinkSummary] {
	response := operation.client.get(call, "/payment_links/"+url.PathEscape(linkID))
	if response.err != nil || response.status == http.StatusTooManyRequests || response.status == http.StatusRequestTimeout || response.status >= 500 {
		return cancelPaymentLinkRetry(sdkgo.FailureAvailability, "Razorpay link state could not be checked", response.retryAfter)
	}
	if response.status != http.StatusOK {
		return cancelPaymentLinkBranch(CancelPaymentLinkBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay rejected link state lookup")
	}
	link, valid := decodePaymentLinkSummary(response.body)
	if !valid {
		return cancelPaymentLinkRetry(sdkgo.FailureProtocol, "Razorpay link state is invalid", 0)
	}
	if link.ID != linkID || link.Status != "cancelled" {
		return cancelPaymentLinkBranch(CancelPaymentLinkBranchProviderRejected, sdkgo.FailureProviderRejection, "Razorpay link is not cancelled")
	}
	return sdkgo.NewMutationBranch(CancelPaymentLinkBranchCancelled, link, nil, sdkgo.Receipt{})
}

func (client *Client) postPaymentLink(call sdkgo.Call, path string, payload []byte) providerResponse {
	credentials, err := client.credentials.Resolve(call)
	if err != nil || credentials.Validate() != nil {
		return providerResponse{err: errCredential}
	}
	keyID, keySecret := credentials.KeyID.Reveal(), credentials.KeySecret.Reveal()
	if !providerhttp.IsHeaderSafeCredential(keyID) || strings.Contains(keyID, ":") || !providerhttp.IsHeaderSafeCredential(keySecret) {
		return providerResponse{err: errCredential}
	}
	request, err := http.NewRequestWithContext(call.Context, http.MethodPost, client.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return providerResponse{err: errRequest}
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(keyID+":"+keySecret)))
	response, err := client.httpClient.Do(request)
	if err != nil {
		return providerResponse{err: errRequest}
	}
	defer func() { _ = response.Body.Close() }()
	body, err := providerhttp.ReadBoundedBody(response.Body, client.maxResponseBytes)
	return providerResponse{body: body, status: response.StatusCode, retryAfter: providerhttp.ParseRetryAfter(response.Header.Get("Retry-After"), time.Now()), err: err}
}

var paymentLinkIDPattern = regexp.MustCompile(`^plink_[A-Za-z0-9]{6,64}$`)

func decodePaymentLinkSummary(body []byte) (PaymentLinkSummary, bool) {
	var wire struct {
		ID          string `json:"id"`
		Status      string `json:"status"`
		Amount      int64  `json:"amount"`
		Currency    string `json:"currency"`
		ReferenceID string `json:"reference_id"`
		ShortURL    string `json:"short_url"`
		UPILink     bool   `json:"upi_link"`
	}
	if json.Unmarshal(body, &wire) != nil || !paymentLinkIDPattern.MatchString(wire.ID) || wire.Status == "" || wire.Amount <= 0 || !validCurrencyCode(wire.Currency) || wire.UPILink {
		return PaymentLinkSummary{}, false
	}
	parsedURL, err := url.Parse(wire.ShortURL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		return PaymentLinkSummary{}, false
	}
	return PaymentLinkSummary{ID: wire.ID, Status: wire.Status, Amount: wire.Amount, CurrencyCode: wire.Currency, ReferenceID: wire.ReferenceID, ShortURL: wire.ShortURL}, true
}

func decodePaymentLinkPage(body []byte) (PaymentLinkPage, bool) {
	var wire struct {
		PaymentLinks []json.RawMessage `json:"payment_links"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.PaymentLinks == nil {
		return PaymentLinkPage{}, false
	}
	page := PaymentLinkPage{Links: make([]PaymentLinkSummary, 0, len(wire.PaymentLinks))}
	for _, item := range wire.PaymentLinks {
		link, valid := decodePaymentLinkSummary(item)
		if !valid {
			return PaymentLinkPage{}, false
		}
		page.Links = append(page.Links, link)
	}
	return page, true
}

func isDuplicatePaymentLinkReference(body []byte) bool {
	var wire struct {
		Error struct {
			Description string `json:"description"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &wire) != nil {
		return false
	}
	description := strings.ToLower(wire.Error.Description)
	return strings.Contains(description, "reference") && (strings.Contains(description, "duplicate") || strings.Contains(description, "already"))
}

func paymentLinkFailure(operation string, kind sdkgo.FailureKind, message string) sdkgo.Failure {
	return sdkgo.Failure{Kind: kind, Provider: "razorpay", Operation: operation, Message: message}
}

func paymentLinkPageBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PaymentLinkPage] {
	failure := paymentLinkFailure("listPaymentLinks", kind, message)
	return sdkgo.NewQueryBranch(branch, PaymentLinkPage{}, &failure, sdkgo.Receipt{})
}

func getPaymentLinkBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.QueryAttempt[PaymentLinkSummary] {
	failure := paymentLinkFailure("getPaymentLink", kind, message)
	return sdkgo.NewQueryBranch(branch, PaymentLinkSummary{}, &failure, sdkgo.Receipt{})
}

func createPaymentLinkBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PaymentLinkSummary] {
	failure := paymentLinkFailure("createPaymentLink", kind, message)
	return sdkgo.NewMutationBranch(branch, PaymentLinkSummary{}, &failure, sdkgo.Receipt{})
}

func createPaymentLinkRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[PaymentLinkSummary] {
	return sdkgo.NewMutationRetry[PaymentLinkSummary](paymentLinkFailure("createPaymentLink", kind, message), retryAfter)
}

func cancelPaymentLinkBranch(branch sdkgo.BranchID, kind sdkgo.FailureKind, message string) sdkgo.MutationAttempt[PaymentLinkSummary] {
	failure := paymentLinkFailure("cancelPaymentLink", kind, message)
	return sdkgo.NewMutationBranch(branch, PaymentLinkSummary{}, &failure, sdkgo.Receipt{})
}

func cancelPaymentLinkRetry(kind sdkgo.FailureKind, message string, retryAfter time.Duration) sdkgo.MutationAttempt[PaymentLinkSummary] {
	return sdkgo.NewMutationRetry[PaymentLinkSummary](paymentLinkFailure("cancelPaymentLink", kind, message), retryAfter)
}
