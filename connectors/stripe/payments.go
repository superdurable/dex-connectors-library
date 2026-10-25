// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package stripe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const maximumStripeErrorMessageRunes = 300

const (
	refundCallIDMetadataKey    = "dex_call_id"
	refundReadbackPageSize     = 100
	maximumRefundReadbackPages = 3
)

// GetPaymentIntentInput identifies one Stripe PaymentIntent.
type GetPaymentIntentInput struct {
	// PaymentIntentID is Stripe's PaymentIntent identifier, beginning with pi_.
	PaymentIntentID string `json:"paymentIntentId"`
}

// PaymentIntent is the connector-safe subset of a Stripe PaymentIntent and its latest charge.
type PaymentIntent struct {
	// ID is Stripe's PaymentIntent identifier.
	ID string `json:"id"`
	// Amount is the intended payment amount in the currency's minor unit.
	Amount int64 `json:"amount"`
	// Currency is the lowercase three-letter currency code.
	Currency string `json:"currency"`
	// Status is Stripe's PaymentIntent status, such as requires_payment_method or succeeded.
	Status string `json:"status"`
	// AmountRefunded is the latest charge's refunded amount in minor units; zero without a charge.
	AmountRefunded int64 `json:"amountRefunded"`
	// CustomerEmail is the receipt email, or the latest charge's billing email when no receipt email is set.
	CustomerEmail string `json:"customerEmail,omitempty"`
	// LatestChargeID is Stripe's identifier of the latest charge when one exists.
	LatestChargeID string `json:"latestChargeId,omitempty"`
}

// CreateRefundInput contains one Stripe refund request.
type CreateRefundInput struct {
	// PaymentIntentID is the Stripe PaymentIntent to refund, beginning with pi_.
	PaymentIntentID string `json:"paymentIntentId"`
	// Amount is the positive refund amount in the currency's minor unit.
	Amount int64 `json:"amount"`
	// Reason optionally classifies the refund as duplicate, fraudulent, or requested_by_customer.
	Reason string `json:"reason,omitempty"`
	// Metadata is bounded non-sensitive application metadata stored on the refund.
	Metadata map[string]string `json:"metadata,omitempty"`
}

// Refund is the connector-safe subset of a Stripe refund.
type Refund struct {
	// RefundID is Stripe's refund identifier, beginning with re_.
	RefundID string `json:"refundId"`
	// Status is succeeded, pending, failed, requires_action, or canceled.
	Status string `json:"status"`
	// Amount is the refunded amount in the currency's minor unit.
	Amount int64 `json:"amount"`
	// Currency is the lowercase three-letter currency code.
	Currency string `json:"currency"`
}

// GetPaymentIntentOperation implements the PaymentIntent query.
type GetPaymentIntentOperation struct{ client *Client }

// CreateRefundOperation implements the refund mutation.
type CreateRefundOperation struct{ client *Client }

type stripePaymentIntent struct {
	ID           string         `json:"id"`
	Object       string         `json:"object"`
	Amount       int64          `json:"amount"`
	Currency     string         `json:"currency"`
	Status       string         `json:"status"`
	ReceiptEmail string         `json:"receipt_email"`
	LatestCharge flexibleCharge `json:"latest_charge"`
}

// flexibleCharge accepts a charge ID, null, or an expanded charge object.
type flexibleCharge struct {
	ID             string
	AmountRefunded int64
	BillingEmail   string
}

type stripeRefund struct {
	ID       string `json:"id"`
	Object   string `json:"object"`
	Status   string `json:"status"`
	Amount   int64  `json:"amount"`
	Currency string `json:"currency"`
}

type stripeAPIError struct {
	Type    string
	Code    string
	Message string
}

// Definition returns the immutable connector operation definition.
func (GetPaymentIntentOperation) Definition() sdkgo.QueryDefinition {
	return GetPaymentIntentDefinition
}

// Invoke retrieves one PaymentIntent with its latest charge expanded.
func (operation GetPaymentIntentOperation) Invoke(call sdkgo.Call, input GetPaymentIntentInput) sdkgo.QueryAttempt[PaymentIntent] {
	paymentIntentID := strings.TrimSpace(input.PaymentIntentID)
	if err := validatePaymentIntentID(paymentIntentID); err != nil {
		return sdkgo.NewQueryBranch(GetPaymentIntentBranchDefect, PaymentIntent{}, stripeFailurePointer("getPaymentIntent", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, "getPaymentIntent")
	if failure != nil {
		return sdkgo.NewQueryBranch(GetPaymentIntentBranchDefect, PaymentIntent{}, failure, sdkgo.Receipt{})
	}
	query := url.Values{"expand[]": {"latest_charge"}}
	response, err := operation.client.request(call, credentials.SecretKey.Reveal(), http.MethodGet, "payment_intents/"+url.PathEscape(paymentIntentID), query)
	if err != nil {
		if errors.Is(err, errStripeResponseTooLarge) {
			return sdkgo.NewQueryBranch(GetPaymentIntentBranchInvalidResponse, PaymentIntent{}, stripeFailurePointer("getPaymentIntent", sdkgo.FailureResponseTooLarge, err.Error()), operation.client.receipt(call, response, paymentIntentID))
		}
		return sdkgo.NewQueryRetry[PaymentIntent](stripeFailure("getPaymentIntent", sdkgo.FailureAvailability, "Stripe PaymentIntent is temporarily unavailable"), 0)
	}
	if response.statusCode == http.StatusTooManyRequests {
		return sdkgo.NewQueryRetry[PaymentIntent](stripeFailure("getPaymentIntent", sdkgo.FailureRateLimit, "Stripe rate limited the PaymentIntent query"), retryAfter(response.header))
	}
	if response.statusCode >= 500 {
		return sdkgo.NewQueryRetry[PaymentIntent](stripeFailure("getPaymentIntent", sdkgo.FailureAvailability, "Stripe PaymentIntent is temporarily unavailable"), 0)
	}
	if response.statusCode == http.StatusNotFound {
		return sdkgo.NewQueryBranch(GetPaymentIntentBranchNotFound, PaymentIntent{}, stripeFailurePointer("getPaymentIntent", sdkgo.FailureNotFound, describeStripeRejection("Stripe PaymentIntent was not found", response)), operation.client.receipt(call, response, paymentIntentID))
	}
	if response.statusCode < 200 || response.statusCode >= 300 {
		return sdkgo.NewQueryBranch(GetPaymentIntentBranchProviderRejected, PaymentIntent{}, classifyStripeRejection("getPaymentIntent", response), operation.client.receipt(call, response, paymentIntentID))
	}
	paymentIntent, err := decodePaymentIntent(response.body)
	if err != nil {
		return sdkgo.NewQueryBranch(GetPaymentIntentBranchInvalidResponse, PaymentIntent{}, stripeFailurePointer("getPaymentIntent", sdkgo.FailureProtocol, err.Error()), operation.client.receipt(call, response, paymentIntentID))
	}
	return sdkgo.NewQueryBranch(GetPaymentIntentBranchFound, paymentIntent, nil, operation.client.receipt(call, response, paymentIntent.ID))
}

// Definition returns the immutable connector operation definition.
func (CreateRefundOperation) Definition() sdkgo.MutationDefinition {
	return CreateRefundDefinition
}

// IdempotencyKey derives Stripe's provider key from the stable connector call ID.
func (CreateRefundOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateRefundInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates one refund, retrying keyed requests when Stripe's response is unavailable.
func (operation CreateRefundOperation) Invoke(call sdkgo.Call, input CreateRefundInput) sdkgo.MutationAttempt[Refund] {
	if err := input.validate(); err != nil {
		return sdkgo.NewMutationBranch(CreateRefundBranchDefect, Refund{}, stripeFailurePointer("createRefund", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	credentials, failure := operation.client.resolveCredentials(call, "createRefund")
	if failure != nil {
		return sdkgo.NewMutationBranch(CreateRefundBranchDefect, Refund{}, failure, sdkgo.Receipt{})
	}
	values := input.formValues()
	values.Set("metadata["+refundCallIDMetadataKey+"]", string(call.ID))
	response, err := operation.client.request(call, credentials.SecretKey.Reveal(), http.MethodPost, "refunds", values)
	isResponseTooLarge := errors.Is(err, errStripeResponseTooLarge)
	if err != nil && !isResponseTooLarge {
		if errors.Is(err, errStripeRequestInvalid) {
			return sdkgo.NewMutationBranch(CreateRefundBranchDefect, Refund{}, stripeFailurePointer("createRefund", sdkgo.FailureLocalDefect, err.Error()), sdkgo.Receipt{})
		}
		return sdkgo.NewMutationRetry[Refund](stripeFailure("createRefund", sdkgo.FailureTransport, "Stripe refund response is unavailable"), retryAfter(response.header))
	}
	if response.statusCode == http.StatusTooManyRequests {
		return sdkgo.NewMutationRetry[Refund](stripeFailure("createRefund", sdkgo.FailureRateLimit, "Stripe rate limited the refund request"), retryAfter(response.header))
	}
	if response.statusCode == http.StatusConflict && shouldRetryKeyedConflict(response) {
		return sdkgo.NewMutationRetry[Refund](stripeFailure("createRefund", sdkgo.FailureConflict, "Stripe is still processing a request with this idempotency key"), retryAfter(response.header))
	}
	if response.statusCode >= 500 {
		if shouldRetryKeyedServerFailure(response) {
			return sdkgo.NewMutationRetry[Refund](stripeFailure("createRefund", sdkgo.FailureAvailability, "Stripe refund is temporarily unavailable"), retryAfter(response.header))
		}
		return operation.readBackRefund(call, credentials.SecretKey.Reveal(), input.PaymentIntentID, response)
	}
	if response.statusCode < 200 || response.statusCode >= 300 {
		return sdkgo.NewMutationBranch(CreateRefundBranchProviderRejected, Refund{}, classifyRefundRejection(response), operation.client.receipt(call, response, ""))
	}
	if isResponseTooLarge {
		return sdkgo.NewMutationBranch(CreateRefundBranchInvalidResponse, Refund{}, stripeFailurePointer("createRefund", sdkgo.FailureResponseTooLarge, err.Error()), operation.client.receipt(call, response, ""))
	}
	refund, err := decodeRefund(response.body)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateRefundBranchInvalidResponse, Refund{}, stripeFailurePointer("createRefund", sdkgo.FailureProtocol, err.Error()), operation.client.receipt(call, response, ""))
	}
	return sdkgo.NewMutationBranch(CreateRefundBranchRefunded, refund, nil, operation.client.receipt(call, response, refund.RefundID))
}

func (operation CreateRefundOperation) readBackRefund(call sdkgo.Call, secretKey string, paymentIntentID string, originalResponse providerResponse) sdkgo.MutationAttempt[Refund] {
	query := url.Values{"payment_intent": {paymentIntentID}, "limit": {strconv.Itoa(refundReadbackPageSize)}}
	for page := 0; page < maximumRefundReadbackPages; page++ {
		response, err := operation.client.request(call, secretKey, http.MethodGet, "refunds", query)
		isResponseTooLarge := errors.Is(err, errStripeResponseTooLarge)
		if (err != nil && !isResponseTooLarge) || response.statusCode == http.StatusTooManyRequests || response.statusCode >= 500 {
			return sdkgo.NewMutationRetry[Refund](stripeFailure("createRefund", sdkgo.FailureAvailability, "Stripe refund read-back is temporarily unavailable"), retryAfter(response.header))
		}
		if response.statusCode == http.StatusUnauthorized || response.statusCode == http.StatusForbidden {
			return operation.deniedRefundReadback(call, originalResponse, response.statusCode)
		}
		if response.statusCode < 200 || response.statusCode >= 300 || isResponseTooLarge {
			break
		}
		var refunds struct {
			Object  string            `json:"object"`
			Data    []json.RawMessage `json:"data"`
			HasMore *bool             `json:"has_more"`
		}
		if json.Unmarshal(response.body, &refunds) != nil || refunds.Object != "list" || refunds.Data == nil || refunds.HasMore == nil || len(refunds.Data) > refundReadbackPageSize {
			break
		}
		lastRefundID := ""
		for _, contents := range refunds.Data {
			var candidate struct {
				ID       string            `json:"id"`
				Metadata map[string]string `json:"metadata"`
			}
			if json.Unmarshal(contents, &candidate) != nil || !strings.HasPrefix(candidate.ID, "re_") {
				return operation.uncertainRefund(call, originalResponse)
			}
			lastRefundID = candidate.ID
			if candidate.Metadata[refundCallIDMetadataKey] == string(call.ID) {
				refund, decodeErr := decodeRefund(contents)
				if decodeErr != nil {
					return operation.uncertainRefund(call, originalResponse)
				}
				return sdkgo.NewMutationBranch(CreateRefundBranchRefunded, refund, nil, operation.client.receipt(call, response, refund.RefundID))
			}
		}
		if !*refunds.HasMore || lastRefundID == "" {
			break
		}
		query.Set("starting_after", lastRefundID)
	}
	return operation.uncertainRefund(call, originalResponse)
}

func (operation CreateRefundOperation) uncertainRefund(call sdkgo.Call, response providerResponse) sdkgo.MutationAttempt[Refund] {
	return sdkgo.NewMutationUncertain(Refund{}, stripeFailure("createRefund", sdkgo.FailureAvailability, "Stripe refund outcome cannot be confirmed after a server error"), operation.client.receipt(call, response, ""))
}

func (operation CreateRefundOperation) deniedRefundReadback(call sdkgo.Call, originalResponse providerResponse, readbackStatusCode int) sdkgo.MutationAttempt[Refund] {
	failure := stripeFailure("createRefund", sdkgo.FailureAuthorization, "Stripe refund outcome cannot be confirmed after a server error because the API key lacks permission for the refund read-back; grant it Refund read permission")
	if readbackStatusCode == http.StatusUnauthorized {
		failure = stripeFailure("createRefund", sdkgo.FailureAuthentication, "Stripe refund outcome cannot be confirmed after a server error because Stripe rejected the API key for the refund read-back; check that the key is still valid")
	}
	return sdkgo.NewMutationUncertain(Refund{}, failure, operation.client.receipt(call, originalResponse, ""))
}

func validatePaymentIntentID(paymentIntentID string) error {
	if !strings.HasPrefix(paymentIntentID, "pi_") || strings.ContainsAny(paymentIntentID, "/?#") {
		return fmt.Errorf("Stripe PaymentIntent ID must be a pi_ identifier")
	}
	return nil
}

func (input CreateRefundInput) validate() error {
	if err := validatePaymentIntentID(input.PaymentIntentID); err != nil {
		return err
	}
	if input.Amount <= 0 {
		return fmt.Errorf("refund amount must be positive")
	}
	switch input.Reason {
	case "", "duplicate", "fraudulent", "requested_by_customer":
	default:
		return fmt.Errorf("refund reason must be duplicate, fraudulent, or requested_by_customer")
	}
	if _, exists := input.Metadata[refundCallIDMetadataKey]; exists {
		return fmt.Errorf("refund metadata key %q is reserved for call reconciliation", refundCallIDMetadataKey)
	}
	if len(input.Metadata) >= maximumMetadataFields {
		return fmt.Errorf("refund metadata cannot contain more than %d application fields", maximumMetadataFields-1)
	}
	return validateMetadata(input.Metadata)
}

func (input CreateRefundInput) formValues() url.Values {
	values := url.Values{
		"payment_intent": {input.PaymentIntentID},
		"amount":         {strconv.FormatInt(input.Amount, 10)},
	}
	if input.Reason != "" {
		values.Set("reason", input.Reason)
	}
	for key, value := range input.Metadata {
		values.Set("metadata["+key+"]", value)
	}
	return values
}

// UnmarshalJSON accepts a charge ID, null, or an expanded charge object.
func (charge *flexibleCharge) UnmarshalJSON(contents []byte) error {
	*charge = flexibleCharge{}
	if bytes.Equal(bytes.TrimSpace(contents), []byte("null")) {
		return nil
	}
	var identifier string
	if err := json.Unmarshal(contents, &identifier); err == nil {
		charge.ID = identifier
		return nil
	}
	var expanded struct {
		ID             string `json:"id"`
		AmountRefunded int64  `json:"amount_refunded"`
		BillingDetails struct {
			Email string `json:"email"`
		} `json:"billing_details"`
	}
	if err := json.Unmarshal(contents, &expanded); err != nil {
		return err
	}
	charge.ID, charge.AmountRefunded, charge.BillingEmail = expanded.ID, expanded.AmountRefunded, expanded.BillingDetails.Email
	return nil
}

func decodePaymentIntent(contents []byte) (PaymentIntent, error) {
	if len(contents) == 0 {
		return PaymentIntent{}, fmt.Errorf("%w: Stripe returned an empty PaymentIntent", errStripeResponseInvalid)
	}
	var decoded stripePaymentIntent
	if err := jsonUnmarshal(contents, &decoded); err != nil {
		return PaymentIntent{}, fmt.Errorf("%w: Stripe returned malformed PaymentIntent JSON", errStripeResponseInvalid)
	}
	if decoded.Object != "payment_intent" || !strings.HasPrefix(decoded.ID, "pi_") || decoded.Currency == "" || decoded.Status == "" {
		return PaymentIntent{}, fmt.Errorf("%w: Stripe returned an invalid PaymentIntent", errStripeResponseInvalid)
	}
	customerEmail := decoded.ReceiptEmail
	if customerEmail == "" {
		customerEmail = decoded.LatestCharge.BillingEmail
	}
	return PaymentIntent{
		ID: decoded.ID, Amount: decoded.Amount, Currency: decoded.Currency, Status: decoded.Status,
		AmountRefunded: decoded.LatestCharge.AmountRefunded, CustomerEmail: customerEmail, LatestChargeID: decoded.LatestCharge.ID,
	}, nil
}

func decodeRefund(contents []byte) (Refund, error) {
	if len(contents) == 0 {
		return Refund{}, fmt.Errorf("%w: Stripe returned an empty refund", errStripeResponseInvalid)
	}
	var decoded stripeRefund
	if err := jsonUnmarshal(contents, &decoded); err != nil {
		return Refund{}, fmt.Errorf("%w: Stripe returned malformed refund JSON", errStripeResponseInvalid)
	}
	switch decoded.Status {
	case "succeeded", "pending", "failed", "requires_action", "canceled":
	default:
		return Refund{}, fmt.Errorf("%w: Stripe returned an invalid refund", errStripeResponseInvalid)
	}
	if decoded.Object != "refund" || !strings.HasPrefix(decoded.ID, "re_") || decoded.Currency == "" {
		return Refund{}, fmt.Errorf("%w: Stripe returned an invalid refund", errStripeResponseInvalid)
	}
	return Refund{RefundID: decoded.ID, Status: decoded.Status, Amount: decoded.Amount, Currency: decoded.Currency}, nil
}

func classifyRefundRejection(response providerResponse) *sdkgo.Failure {
	failure := classifyStripeRejection("createRefund", response)
	failure.Message = describeStripeRejection("Stripe rejected the refund", response)
	return failure
}

// describeStripeRejection appends Stripe's error code and message, except for credential errors
// whose message can echo part of the API key.
func describeStripeRejection(summary string, response providerResponse) string {
	if response.statusCode == http.StatusUnauthorized || response.statusCode == http.StatusForbidden {
		return summary
	}
	apiError := parseStripeAPIError(response.body)
	if apiError.Code != "" {
		summary += ": " + apiError.Code
	}
	if apiError.Message != "" {
		message := apiError.Message
		if runes := []rune(message); len(runes) > maximumStripeErrorMessageRunes {
			message = string(runes[:maximumStripeErrorMessageRunes])
		}
		summary += ": " + message
	}
	return summary
}

func parseStripeAPIError(contents []byte) stripeAPIError {
	var decoded struct {
		Error struct {
			Type    string `json:"type"`
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(contents, &decoded); err != nil {
		return stripeAPIError{}
	}
	return stripeAPIError{Type: decoded.Error.Type, Code: decoded.Error.Code, Message: decoded.Error.Message}
}
