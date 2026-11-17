// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package activecampaign

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateEcommerceCustomerInput creates one Deep Data store customer.
type CreateEcommerceCustomerInput struct {
	// ConnectionID is the numeric ecommerce connection ID.
	ConnectionID string `json:"connectionId"`
	// ExternalID is the customer's ID in the external store.
	ExternalID string `json:"externalId"`
	// Email is the customer's email address.
	Email string `json:"email"`
}

// CreateEcommerceCustomerOperation creates one customer with bounded sends.
type CreateEcommerceCustomerOperation struct{ client *Client }

type ecommerceCustomerCreateDispatch struct {
	client     *Client
	call       sdkgo.Call
	body       []byte
	response   []byte
	status     int
	retryAfter time.Duration
	err        error
}

// CreateEcommerceCustomer binds the create Mutation.
func (client *Client) CreateEcommerceCustomer() CreateEcommerceCustomerOperation {
	return CreateEcommerceCustomerOperation{client}
}

// Definition returns the manifest-generated create contract.
func (CreateEcommerceCustomerOperation) Definition() sdkgo.MutationDefinition {
	return CreateEcommerceCustomerDefinition
}

// IdempotencyKey correlates bounded sends locally; the provider receives no key.
func (CreateEcommerceCustomerOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateEcommerceCustomerInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

func (operation CreateEcommerceCustomerOperation) Invoke(call sdkgo.Call, input CreateEcommerceCustomerInput) sdkgo.MutationAttempt[EcommerceCustomerMetadata] {
	if !numericID.MatchString(input.ConnectionID) || strings.TrimSpace(input.ExternalID) == "" || len(input.ExternalID) > 250 || strings.ContainsAny(input.ExternalID, "\r\n") || !validContactEmail(input.Email) {
		return ecommerceCustomerMutationBranch("createEcommerceCustomer", CreateEcommerceCustomerBranchDefect, sdkgo.FailureValidation, "connection ID, external ID, or email is invalid")
	}
	fields := map[string]string{"connectionid": input.ConnectionID, "externalid": input.ExternalID, "email": input.Email}
	encoded, err := json.Marshal(map[string]any{"ecomCustomer": fields})
	if err != nil {
		return ecommerceCustomerMutationBranch("createEcommerceCustomer", CreateEcommerceCustomerBranchDefect, sdkgo.FailureLocalDefect, "customer request could not be encoded")
	}
	fence, err := singledispatch.NewFence(call, CreateEcommerceCustomerDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return ecommerceCustomerMutationBranch("createEcommerceCustomer", CreateEcommerceCustomerBranchDefect, sdkgo.FailureLocalDefect, "customer checkpoint could not be initialized")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return ecommerceCustomerMutationBranch("createEcommerceCustomer", CreateEcommerceCustomerBranchDefect, sdkgo.FailureLocalDefect, "customer checkpoint is invalid")
	}
	if decision.MaximumPriorSends > 0 || decision.Marker.ProviderID != "" {
		found, state := operation.readBack(call, input, decision.Marker.ProviderID)
		switch state {
		case "found":
			return sdkgo.NewMutationBranch(CreateEcommerceCustomerBranchCreated, found, nil, dealReceipt(call, found.ID))
		case "retry":
			return sdkgo.NewMutationRetry[EcommerceCustomerMetadata](queryFailure("createEcommerceCustomer", sdkgo.FailureAvailability, "customer read-back unavailable"), 0)
		case "uncertain":
			return ecommerceCustomerCreateUncertain(call, "customer read-back cannot uniquely confirm the earlier create")
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return ecommerceCustomerCreateUncertain(call, "an earlier customer create may have reached the send cap")
	}
	dispatch := &ecommerceCustomerCreateDispatch{client: operation.client, call: call, body: encoded}
	if err := fence.Dispatch(dispatch.execute); err != nil {
		return sdkgo.NewMutationRetry[EcommerceCustomerMetadata](queryFailure("createEcommerceCustomer", sdkgo.FailureAvailability, "customer checkpoint unavailable"), 0)
	}
	if errors.Is(dispatch.err, errCredential) {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[EcommerceCustomerMetadata](queryFailure("createEcommerceCustomer", sdkgo.FailureAvailability, "customer checkpoint unavailable"), 0)
		}
		return ecommerceCustomerMutationBranch("createEcommerceCustomer", CreateEcommerceCustomerBranchDefect, sdkgo.FailureAuthentication, "connection credentials unavailable")
	}
	if errors.Is(dispatch.err, errRequest) {
		return sdkgo.NewMutationRetry[EcommerceCustomerMetadata](queryFailure("createEcommerceCustomer", sdkgo.FailureTransport, "customer create outcome is unknown"), 0)
	}
	if dispatch.status == http.StatusTooManyRequests || dispatch.status == http.StatusServiceUnavailable {
		if err := fence.RecordCanResend(); err != nil {
			return sdkgo.NewMutationRetry[EcommerceCustomerMetadata](queryFailure("createEcommerceCustomer", sdkgo.FailureAvailability, "customer checkpoint unavailable"), 0)
		}
		kind := sdkgo.FailureRateLimit
		if dispatch.status == http.StatusServiceUnavailable {
			kind = sdkgo.FailureAvailability
		}
		return sdkgo.NewMutationRetry[EcommerceCustomerMetadata](queryFailure("createEcommerceCustomer", kind, "ActiveCampaign temporarily rejected customer creation"), dispatch.retryAfter)
	}
	if dispatch.status == http.StatusRequestTimeout || dispatch.status >= 500 {
		return sdkgo.NewMutationRetry[EcommerceCustomerMetadata](queryFailure("createEcommerceCustomer", sdkgo.FailureAvailability, "customer create outcome is unknown"), dispatch.retryAfter)
	}
	if dispatch.status != http.StatusCreated && dispatch.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return ecommerceCustomerCreateUncertain(call, "earlier customer create may have succeeded")
		}
		return ecommerceCustomerMutationBranch("createEcommerceCustomer", CreateEcommerceCustomerBranchProviderRejected, sdkgo.FailureProviderRejection, "ActiveCampaign rejected HTTP "+strconv.Itoa(dispatch.status))
	}
	if dispatch.err != nil {
		return ecommerceCustomerCreateUncertain(call, "customer create response was interrupted")
	}
	customer, err := decodeEcommerceCustomer(dispatch.response)
	if err != nil {
		return ecommerceCustomerCreateUncertain(call, "customer create response was invalid")
	}
	if !matchesEcommerceCustomer(customer, input) {
		return ecommerceCustomerCreateUncertain(call, "customer create response does not match the request")
	}
	_ = fence.RecordProviderID(customer.ID)
	return sdkgo.NewMutationBranch(CreateEcommerceCustomerBranchCreated, customer, nil, dealReceipt(call, customer.ID))
}

func (operation CreateEcommerceCustomerOperation) readBack(call sdkgo.Call, input CreateEcommerceCustomerInput, providerID string) (EcommerceCustomerMetadata, string) {
	path := "/ecomCustomers/" + providerID
	if providerID == "" {
		path = "/ecomCustomers?" + url.Values{"filters[connectionid]": {input.ConnectionID}, "filters[externalid]": {input.ExternalID}}.Encode()
	}
	body, status, _, err := operation.client.get(call, path)
	if err != nil || status == http.StatusTooManyRequests || status >= 500 {
		return EcommerceCustomerMetadata{}, "retry"
	}
	if status == http.StatusNotFound {
		return EcommerceCustomerMetadata{}, "absent"
	}
	if status != http.StatusOK {
		return EcommerceCustomerMetadata{}, "uncertain"
	}
	if providerID != "" {
		customer, err := decodeEcommerceCustomer(body)
		if err != nil || !matchesEcommerceCustomer(customer, input) {
			return EcommerceCustomerMetadata{}, "uncertain"
		}
		return customer, "found"
	}
	var wire struct {
		EcomCustomers []ecommerceCustomerWire `json:"ecomCustomers"`
		Meta          struct {
			Total string `json:"total"`
		} `json:"meta"`
	}
	if err := json.Unmarshal(body, &wire); err != nil || wire.EcomCustomers == nil {
		return EcommerceCustomerMetadata{}, "uncertain"
	}
	total, err := strconv.Atoi(wire.Meta.Total)
	if err != nil || total > len(wire.EcomCustomers) {
		return EcommerceCustomerMetadata{}, "uncertain"
	}
	var match EcommerceCustomerMetadata
	for _, item := range wire.EcomCustomers {
		customer, err := ecommerceCustomerMetadata(item)
		if err != nil {
			return EcommerceCustomerMetadata{}, "uncertain"
		}
		if customer.ConnectionID == input.ConnectionID && customer.ExternalID == input.ExternalID {
			if !matchesEcommerceCustomer(customer, input) {
				return EcommerceCustomerMetadata{}, "uncertain"
			}
			if match.ID != "" {
				return EcommerceCustomerMetadata{}, "uncertain"
			}
			match = customer
		}
	}
	if match.ID != "" {
		return match, "found"
	}
	return EcommerceCustomerMetadata{}, "absent"
}

func matchesEcommerceCustomer(customer EcommerceCustomerMetadata, input CreateEcommerceCustomerInput) bool {
	return customer.ConnectionID == input.ConnectionID && customer.ExternalID == input.ExternalID && customer.Email == input.Email
}

func (dispatch *ecommerceCustomerCreateDispatch) execute(ctx context.Context) error {
	dispatch.response, dispatch.status, dispatch.retryAfter, dispatch.err = dispatch.client.requestWithContext(ctx, dispatch.call, http.MethodPost, "/ecomCustomers", dispatch.body)
	return nil
}
func ecommerceCustomerCreateUncertain(call sdkgo.Call, message string) sdkgo.MutationAttempt[EcommerceCustomerMetadata] {
	return sdkgo.NewMutationUncertain(EcommerceCustomerMetadata{}, queryFailure("createEcommerceCustomer", sdkgo.FailureTransport, message), dealReceipt(call, ""))
}
