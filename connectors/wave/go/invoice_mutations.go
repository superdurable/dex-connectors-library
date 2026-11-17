// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package wave

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

const createInvoiceGraphQL = "mutation CreateInvoice($input: InvoiceCreateInput!) { invoiceCreate(input: $input) { didSucceed inputErrors { code } invoice { id status title invoiceNumber customer { id } } } }"
const updateInvoiceGraphQL = "mutation UpdateInvoice($input: InvoicePatchInput!) { invoicePatch(input: $input) { didSucceed inputErrors { code } invoice { id status title invoiceNumber customer { id } } } }"
const approveInvoiceGraphQL = "mutation ApproveInvoice($input: InvoiceApproveInput!) { invoiceApprove(input: $input) { didSucceed inputErrors { code } invoice { id status title invoiceNumber customer { id } } } }"
const deleteInvoiceGraphQL = "mutation DeleteInvoice($input: InvoiceDeleteInput!) { invoiceDelete(input: $input) { didSucceed inputErrors { code } } }"
const sendInvoiceGraphQL = "mutation SendInvoice($input: InvoiceSendInput!) { invoiceSend(input: $input) { didSucceed inputErrors { code } } }"

// CreateInvoiceInput creates one invoice line for a customer and product.
type CreateInvoiceInput struct {
	// BusinessID is the owning Wave business ID.
	BusinessID string `json:"businessId"`
	// CustomerID is the Wave customer ID.
	CustomerID string `json:"customerId"`
	// ProductID is the Wave product ID for the first line item.
	ProductID string `json:"productId"`
}

// UpdateInvoiceInput replaces an invoice title.
type UpdateInvoiceInput struct {
	// BusinessID identifies the business for the returned profile.
	BusinessID string `json:"businessId"`
	// InvoiceID is the exact Wave invoice ID.
	InvoiceID string `json:"invoiceId"`
	// Title is the replacement invoice title.
	Title string `json:"title"`
}

// ApproveInvoiceInput identifies an invoice to approve.
type ApproveInvoiceInput struct {
	// BusinessID identifies the business for the returned profile.
	BusinessID string `json:"businessId"`
	// InvoiceID is the exact Wave invoice ID.
	InvoiceID string `json:"invoiceId"`
}

// DeleteInvoiceInput identifies an invoice to delete.
type DeleteInvoiceInput struct {
	// InvoiceID is the exact Wave invoice ID.
	InvoiceID string `json:"invoiceId"`
}

// DeletedInvoice identifies an invoice removed by Wave.
type DeletedInvoice struct {
	// ID is the deleted Wave invoice ID.
	ID string `json:"id"`
}

// SendInvoiceInput addresses an approved invoice email.
type SendInvoiceInput struct {
	// InvoiceID is the exact approved Wave invoice ID.
	InvoiceID string `json:"invoiceId"`
	// To contains recipient email addresses.
	To []string `json:"to"`
	// AttachPDF includes an invoice PDF in the email when true.
	AttachPDF bool `json:"attachPDF"`
}

// SentInvoice identifies an invoice queued for email delivery.
type SentInvoice struct {
	// ID is the Wave invoice ID queued for sending.
	ID string `json:"id"`
}

// CreateInvoiceOperation creates an invoice with bounded possible sends.
type CreateInvoiceOperation struct{ client *Client }

// CreateInvoice binds invoice creation to this client.
func (client *Client) CreateInvoice() CreateInvoiceOperation { return CreateInvoiceOperation{client} }

// Definition returns the generated Mutation contract.
func (CreateInvoiceOperation) Definition() sdkgo.MutationDefinition { return CreateInvoiceDefinition }

// IdempotencyKey identifies one logical invoice creation.
func (CreateInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateInvoiceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke creates an invoice for one customer and product.
func (operation CreateInvoiceOperation) Invoke(call sdkgo.Call, input CreateInvoiceInput) sdkgo.MutationAttempt[InvoiceProfile] {
	if !validGraphQLID(input.BusinessID) || !validGraphQLID(input.CustomerID) || !validGraphQLID(input.ProductID) {
		return waveMutationBranch[InvoiceProfile]("createInvoice", "defect", sdkgo.FailureValidation, "Wave invoice input is invalid")
	}
	variables := struct {
		Input struct {
			BusinessID string `json:"businessId"`
			CustomerID string `json:"customerId"`
			Items      []struct {
				ProductID string `json:"productId"`
			} `json:"items"`
		} `json:"input"`
	}{}
	variables.Input.BusinessID, variables.Input.CustomerID = input.BusinessID, input.CustomerID
	variables.Input.Items = []struct {
		ProductID string `json:"productId"`
	}{{ProductID: input.ProductID}}
	return invokeLimitedInvoiceMutation(operation.client, call, "createInvoice", CreateInvoiceDefinition, createInvoiceGraphQL, variables, "created", func(body []byte) (InvoiceProfile, bool, bool) {
		wire, didSucceed, isValid := decodeInvoiceMutation(body, "invoiceCreate")
		if !isValid || !didSucceed {
			return InvoiceProfile{}, didSucceed, isValid
		}
		profile, isValid := wire.profile(input.BusinessID)
		return profile, didSucceed, isValid && profile.CustomerID == input.CustomerID
	})
}

// UpdateInvoiceOperation replaces an invoice title.
type UpdateInvoiceOperation struct{ client *Client }

// UpdateInvoice binds an absolute title update to this client.
func (client *Client) UpdateInvoice() UpdateInvoiceOperation { return UpdateInvoiceOperation{client} }

// Definition returns the generated Mutation contract.
func (UpdateInvoiceOperation) Definition() sdkgo.MutationDefinition { return UpdateInvoiceDefinition }

// IdempotencyKey identifies retries of one title update.
func (UpdateInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateInvoiceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke replaces the invoice title by ID.
func (operation UpdateInvoiceOperation) Invoke(call sdkgo.Call, input UpdateInvoiceInput) sdkgo.MutationAttempt[InvoiceProfile] {
	if !validGraphQLID(input.BusinessID) || !validGraphQLID(input.InvoiceID) || !validCustomerName(input.Title) {
		return waveMutationBranch[InvoiceProfile]("updateInvoice", "defect", sdkgo.FailureValidation, "Wave invoice input is invalid")
	}
	variables := struct {
		Input struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"input"`
	}{}
	variables.Input.ID, variables.Input.Title = input.InvoiceID, input.Title
	return executeInvoiceProfileMutation(operation.client, call, invoiceProfileMutationSpec{operation: "updateInvoice", query: updateInvoiceGraphQL, variables: variables, field: "invoicePatch", branch: "updated", businessID: input.BusinessID, invoiceID: input.InvoiceID, expectedTitle: input.Title})
}

// ApproveInvoiceOperation approves a saved invoice.
type ApproveInvoiceOperation struct{ client *Client }

// ApproveInvoice binds invoice approval to this client.
func (client *Client) ApproveInvoice() ApproveInvoiceOperation {
	return ApproveInvoiceOperation{client}
}

// Definition returns the generated Mutation contract.
func (ApproveInvoiceOperation) Definition() sdkgo.MutationDefinition { return ApproveInvoiceDefinition }

// IdempotencyKey identifies retries of one approval.
func (ApproveInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ ApproveInvoiceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke approves an invoice by ID.
func (operation ApproveInvoiceOperation) Invoke(call sdkgo.Call, input ApproveInvoiceInput) sdkgo.MutationAttempt[InvoiceProfile] {
	if !validGraphQLID(input.BusinessID) || !validGraphQLID(input.InvoiceID) {
		return waveMutationBranch[InvoiceProfile]("approveInvoice", "defect", sdkgo.FailureValidation, "Wave invoice IDs are invalid")
	}
	variables := struct {
		Input struct {
			InvoiceID string `json:"invoiceId"`
		} `json:"input"`
	}{}
	variables.Input.InvoiceID = input.InvoiceID
	return executeInvoiceProfileMutation(operation.client, call, invoiceProfileMutationSpec{operation: "approveInvoice", query: approveInvoiceGraphQL, variables: variables, field: "invoiceApprove", branch: "approved", businessID: input.BusinessID, invoiceID: input.InvoiceID})
}

// DeleteInvoiceOperation deletes an invoice by ID.
type DeleteInvoiceOperation struct{ client *Client }

// DeleteInvoice binds invoice deletion to this client.
func (client *Client) DeleteInvoice() DeleteInvoiceOperation { return DeleteInvoiceOperation{client} }

// Definition returns the generated Mutation contract.
func (DeleteInvoiceOperation) Definition() sdkgo.MutationDefinition { return DeleteInvoiceDefinition }

// IdempotencyKey identifies retries of one deletion.
func (DeleteInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ DeleteInvoiceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke deletes an invoice by ID.
func (operation DeleteInvoiceOperation) Invoke(call sdkgo.Call, input DeleteInvoiceInput) sdkgo.MutationAttempt[DeletedInvoice] {
	if !validGraphQLID(input.InvoiceID) {
		return waveMutationBranch[DeletedInvoice]("deleteInvoice", "defect", sdkgo.FailureValidation, "Wave invoice ID is invalid")
	}
	variables := struct {
		Input struct {
			InvoiceID string `json:"invoiceId"`
		} `json:"input"`
	}{}
	variables.Input.InvoiceID = input.InvoiceID
	response := operation.client.sendGraphQL(call, call.Context, deleteInvoiceGraphQL, variables, false)
	if retry, hasRetry := waveMutationResponseRetry[DeletedInvoice]("deleteInvoice", response); hasRetry {
		return retry
	}
	if errors.Is(response.err, errCredential) {
		return waveMutationBranch[DeletedInvoice]("deleteInvoice", "defect", sdkgo.FailureAuthentication, "Wave credentials are invalid")
	}
	if response.status != http.StatusOK {
		return waveMutationBranch[DeletedInvoice]("deleteInvoice", "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected invoice deletion")
	}
	if response.err != nil {
		return waveMutationBranch[DeletedInvoice]("deleteInvoice", "invalidResponse", sdkgo.FailureResponseTooLarge, "Wave invoice response exceeded the limit")
	}
	didSucceed, isValid := decodeInvoiceStatusMutation(response.body, "invoiceDelete")
	if !isValid {
		return waveMutationBranch[DeletedInvoice]("deleteInvoice", "invalidResponse", sdkgo.FailureProtocol, "Wave returned invalid invoice JSON")
	}
	if !didSucceed {
		return waveMutationBranch[DeletedInvoice]("deleteInvoice", "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected invoice deletion")
	}
	return sdkgo.NewMutationBranch(sdkgo.BranchID("deleted"), DeletedInvoice{ID: input.InvoiceID}, nil, sdkgo.Receipt{})
}

// SendInvoiceOperation emails an invoice with bounded possible sends.
type SendInvoiceOperation struct{ client *Client }

// SendInvoice binds invoice email delivery to this client.
func (client *Client) SendInvoice() SendInvoiceOperation { return SendInvoiceOperation{client} }

// Definition returns the generated Mutation contract.
func (SendInvoiceOperation) Definition() sdkgo.MutationDefinition { return SendInvoiceDefinition }

// IdempotencyKey identifies one logical invoice email.
func (SendInvoiceOperation) IdempotencyKey(callID sdkgo.CallID, _ SendInvoiceInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke queues an approved invoice email.
func (operation SendInvoiceOperation) Invoke(call sdkgo.Call, input SendInvoiceInput) sdkgo.MutationAttempt[SentInvoice] {
	if !validGraphQLID(input.InvoiceID) || len(input.To) == 0 || len(input.To) > 20 {
		return waveMutationBranch[SentInvoice]("sendInvoice", "defect", sdkgo.FailureValidation, "Wave invoice send input is invalid")
	}
	for _, address := range input.To {
		if len(address) > 254 || !strings.Contains(address, "@") || strings.ContainsAny(address, "\r\n") {
			return waveMutationBranch[SentInvoice]("sendInvoice", "defect", sdkgo.FailureValidation, "Wave invoice recipient is invalid")
		}
	}
	variables := struct {
		Input SendInvoiceInput `json:"input"`
	}{input}
	return invokeLimitedInvoiceMutation(operation.client, call, "sendInvoice", SendInvoiceDefinition, sendInvoiceGraphQL, variables, "sent", func(body []byte) (SentInvoice, bool, bool) {
		didSucceed, isValid := decodeInvoiceStatusMutation(body, "invoiceSend")
		return SentInvoice{ID: input.InvoiceID}, didSucceed, isValid
	})
}

type invoiceProfileMutationSpec struct {
	operation     string
	query         string
	variables     any
	field         string
	branch        sdkgo.BranchID
	businessID    string
	invoiceID     string
	expectedTitle string
}

func executeInvoiceProfileMutation(client *Client, call sdkgo.Call, spec invoiceProfileMutationSpec) sdkgo.MutationAttempt[InvoiceProfile] {
	response := client.sendGraphQL(call, call.Context, spec.query, spec.variables, false)
	if retry, hasRetry := waveMutationResponseRetry[InvoiceProfile](spec.operation, response); hasRetry {
		return retry
	}
	if errors.Is(response.err, errCredential) {
		return waveMutationBranch[InvoiceProfile](spec.operation, "defect", sdkgo.FailureAuthentication, "Wave credentials are invalid")
	}
	if response.status != http.StatusOK {
		return waveMutationBranch[InvoiceProfile](spec.operation, "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected invoice change")
	}
	if response.err != nil {
		return waveMutationBranch[InvoiceProfile](spec.operation, "invalidResponse", sdkgo.FailureResponseTooLarge, "Wave invoice response exceeded the limit")
	}
	wire, didSucceed, isValid := decodeInvoiceMutation(response.body, spec.field)
	if !isValid {
		return waveMutationBranch[InvoiceProfile](spec.operation, "invalidResponse", sdkgo.FailureProtocol, "Wave returned invalid invoice JSON")
	}
	if !didSucceed {
		return waveMutationBranch[InvoiceProfile](spec.operation, "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected invoice change")
	}
	profile, isValid := wire.profile(spec.businessID)
	if !isValid || profile.ID != spec.invoiceID || spec.expectedTitle != "" && profile.Title != spec.expectedTitle {
		return waveMutationBranch[InvoiceProfile](spec.operation, "invalidResponse", sdkgo.FailureProtocol, "Wave returned inconsistent invoice data")
	}
	return sdkgo.NewMutationBranch(spec.branch, profile, nil, sdkgo.Receipt{})
}

func invokeLimitedInvoiceMutation[T any](client *Client, call sdkgo.Call, operation string, definition sdkgo.MutationDefinition, query string, variables any, successBranch sdkgo.BranchID, decode func([]byte) (T, bool, bool)) sdkgo.MutationAttempt[T] {
	fence, err := singledispatch.NewFence(call, definition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return waveMutationBranch[T](operation, "defect", sdkgo.FailureLocalDefect, "Wave dispatch failed")
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed || decision.Kind == singledispatch.ReadBackRequired {
		return uncertainInvoice[T](operation)
	}
	canResend := decision.MaximumPriorSends+1 < call.MaxSends()
	var response providerResponse
	err = fence.Dispatch(func(requestContext context.Context) error {
		response = client.sendGraphQL(call, requestContext, query, variables, true)
		if errors.Is(response.err, errCredential) {
			return nil
		}
		return response.err
	})
	if errors.Is(err, singledispatch.ErrCheckpoint) || errors.Is(err, singledispatch.ErrInsufficientTime) {
		return waveMutationRetry[T](operation, sdkgo.FailureAvailability)
	}
	if errors.Is(response.err, errCredential) {
		return uncertainInvoice[T](operation)
	}
	if response.status == http.StatusTooManyRequests || response.status == http.StatusServiceUnavailable {
		if fence.RecordCanResend() != nil {
			return waveMutationRetry[T](operation, sdkgo.FailureAvailability)
		}
		return sdkgo.NewMutationRetry[T](waveFailure(operation, sdkgo.FailureRateLimit, "Wave rate limited invoice operation"), response.retryAfter)
	}
	if response.status == 0 || response.status == http.StatusRequestTimeout || response.status >= 500 || err != nil {
		if canResend {
			return waveMutationRetry[T](operation, sdkgo.FailureAvailability)
		}
		return uncertainInvoice[T](operation)
	}
	if response.status != http.StatusOK {
		if decision.MaximumPriorSends > 0 {
			return uncertainInvoice[T](operation)
		}
		return waveMutationBranch[T](operation, "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected invoice operation")
	}
	value, didSucceed, isValid := decode(response.body)
	if !isValid {
		return uncertainInvoice[T](operation)
	}
	if !didSucceed {
		if decision.MaximumPriorSends > 0 {
			return uncertainInvoice[T](operation)
		}
		return waveMutationBranch[T](operation, "providerRejected", sdkgo.FailureProviderRejection, "Wave rejected invoice operation")
	}
	return sdkgo.NewMutationBranch(successBranch, value, nil, sdkgo.Receipt{})
}

func decodeInvoiceMutation(body []byte, field string) (invoiceWire, bool, bool) {
	var wire struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors []json.RawMessage          `json:"errors"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Data == nil || len(wire.Errors) > 0 {
		return invoiceWire{}, false, false
	}
	var result struct {
		DidSucceed bool         `json:"didSucceed"`
		Invoice    *invoiceWire `json:"invoice"`
	}
	if json.Unmarshal(wire.Data[field], &result) != nil || result.Invoice == nil && result.DidSucceed {
		return invoiceWire{}, false, false
	}
	if result.Invoice == nil {
		return invoiceWire{}, result.DidSucceed, true
	}
	return *result.Invoice, result.DidSucceed, true
}
func decodeInvoiceStatusMutation(body []byte, field string) (bool, bool) {
	var wire struct {
		Data   map[string]json.RawMessage `json:"data"`
		Errors []json.RawMessage          `json:"errors"`
	}
	if json.Unmarshal(body, &wire) != nil || wire.Data == nil || len(wire.Errors) > 0 {
		return false, false
	}
	var result struct {
		DidSucceed bool `json:"didSucceed"`
	}
	if json.Unmarshal(wire.Data[field], &result) != nil {
		return false, false
	}
	return result.DidSucceed, true
}
func uncertainInvoice[T any](operation string) sdkgo.MutationAttempt[T] {
	failure := waveFailure(operation, sdkgo.FailureTransport, "Wave invoice operation may have succeeded but could not be confirmed")
	return sdkgo.NewMutationUncertain(*new(T), failure, sdkgo.Receipt{})
}
