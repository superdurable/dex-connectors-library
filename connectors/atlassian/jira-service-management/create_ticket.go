// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package jiraservicemanagement

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	createTicketOperationID    = "createTicket"
	createTicketFailureSubject = "customer request"
	maximumSummaryCharacters   = 255
	// MaxAdditionalFieldValues bounds CreateTicketInput.AdditionalFieldValues.
	MaxAdditionalFieldValues   = 20
	maximumFieldValueJSONBytes = 16 << 10
)

// CreateTicketInput is one new customer request. The request type decides which fields Jira Service
// Management requires; GET /rest/servicedeskapi/servicedesk/{serviceDeskId}/requesttype/{requestTypeId}/field
// lists them.
type CreateTicketInput struct {
	// ServiceDeskID is the numeric service desk ID, as the request type picker stores it.
	ServiceDeskID string `json:"serviceDeskId"`
	// RequestTypeID is the numeric request type ID, as the request type picker stores it.
	RequestTypeID string `json:"requestTypeId"`
	// Summary is the one-line request title, at most 255 characters.
	Summary string `json:"summary"`
	// Description is the request's text, at most 32767 characters; blank sends none. Jira Service
	// Management stores it as text in which it renders wiki markup, so characters such as * and _ can
	// format the text; the connector sends it unchanged.
	Description string `json:"description,omitempty"`
	// RaiseOnBehalfOfAccountID raises the request for this customer, by Atlassian account ID such as one
	// findCustomerByEmail returned; blank raises it as the connected agent.
	RaiseOnBehalfOfAccountID string `json:"raiseOnBehalfOfAccountId,omitempty"`
	// AdditionalFieldValues sets up to MaxAdditionalFieldValues more request-type fields by Jira field ID,
	// such as customfield_10050, each as the JSON value Jira's field input format expects. It cannot set
	// summary or description.
	AdditionalFieldValues map[string]json.RawMessage `json:"additionalFieldValues,omitempty"`
}

// CreateTicketOutput identifies the created request. On providerRejected and uncertain, IssueID and
// IssueKey are empty and the requested desk, request type, summary, and customer are echoed so the
// application can reconcile.
type CreateTicketOutput struct {
	// IssueID is the new request's numeric issue ID.
	IssueID string `json:"issueId,omitempty"`
	// IssueKey is the new request's key, such as ITH-12.
	IssueKey string `json:"issueKey,omitempty"`
	// ServiceDeskID echoes the requested service desk.
	ServiceDeskID string `json:"serviceDeskId"`
	// RequestTypeID echoes the requested request type.
	RequestTypeID string `json:"requestTypeId"`
	// Summary echoes the requested summary.
	Summary string `json:"summary"`
	// RaiseOnBehalfOfAccountID echoes the customer the request was raised for.
	RaiseOnBehalfOfAccountID string `json:"raiseOnBehalfOfAccountId,omitempty"`
	// RequestStatus is the new request's Jira Service Management status, or nil when the provider gave none.
	RequestStatus *RequestStatus `json:"requestStatus,omitempty"`
	// CreatedAt is when the request was raised, or nil when the provider gave no date.
	CreatedAt *time.Time `json:"createdAt,omitempty"`
	// RejectedFieldIDs lists the field IDs a rejection named.
	RejectedFieldIDs []string `json:"rejectedFieldIds,omitempty"`
	// ProviderErrorKey is the machine-readable i18n key of a rejection, when the provider sent a safe one.
	ProviderErrorKey string `json:"providerErrorKey,omitempty"`
}

// RequestStatus is a customer request's status as the request API reports it.
type RequestStatus struct {
	// Name is the status name, such as Waiting for support.
	Name string `json:"name"`
	// Category is the request API's status category.
	Category RequestStatusCategory `json:"category,omitempty"`
}

// CreateTicketOperation implements the createTicket Mutation.
type CreateTicketOperation struct{ client *Client }

type createTicketRequestBody struct {
	ServiceDeskID      string                     `json:"serviceDeskId"`
	RequestTypeID      string                     `json:"requestTypeId"`
	RequestFieldValues map[string]json.RawMessage `json:"requestFieldValues"`
	RaiseOnBehalfOf    string                     `json:"raiseOnBehalfOf,omitempty"`
}

type createdRequestResource struct {
	IssueID       string           `json:"issueId"`
	IssueKey      string           `json:"issueKey"`
	CreatedDate   *serviceDeskDate `json:"createdDate"`
	CurrentStatus *struct {
		Status         string `json:"status"`
		StatusCategory string `json:"statusCategory"`
	} `json:"currentStatus"`
}

// Definition returns the immutable connector operation definition.
func (CreateTicketOperation) Definition() sdkgo.MutationDefinition { return CreateTicketDefinition }

// IdempotencyKey derives the key recorded in the Receipt from the stable call ID. Jira Service Management
// accepts no idempotency key, so it is never sent; single dispatch comes from a Dex heartbeat checkpoint.
func (CreateTicketOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateTicketInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends POST /rest/servicedeskapi/request at most once per Step execution. Only a 429 or a
// connection that never opened is retried; any other unconfirmed outcome selects uncertain.
func (operation CreateTicketOperation) Invoke(call sdkgo.Call, input CreateTicketInput) sdkgo.MutationAttempt[CreateTicketOutput] {
	client := operation.client
	body, requested, err := buildCreateTicketRequest(input)
	if err != nil {
		return sdkgo.NewMutationBranch(CreateTicketBranchDefect, requested, failurePointer(createTicketOperationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, sessionErr := client.startSession(call, createTicketOperationID)
	if sessionErr != nil {
		if sessionErr.isRetryable {
			return sdkgo.NewMutationRetry[CreateTicketOutput](sessionErr.failure, sessionErr.retryAfter)
		}
		return sdkgo.NewMutationBranch(CreateTicketBranchDefect, requested, sessionErr.pointer(), sdkgo.Receipt{})
	}
	defer cancel()
	if attempt, isTerminal := singleDispatchAttemptBeforeSend(claimSingleDispatch(call), createTicketOperationID, requested, client.emptyReceipt(session)); isTerminal {
		return attempt
	}
	result := client.exchange(session, providerRequest{method: http.MethodPost, api: serviceDeskAPI, path: "/request", payload: body})
	classification := client.classifyUnkeyedWrite(createTicketOperationID, createTicketFailureSubject, result)
	receipt := client.receipt(session, result.response, "")
	if classification.outcome == writeRejected {
		requested.RejectedFieldIDs, requested.ProviderErrorKey = classification.rejection.fieldIDs, classification.rejection.errorKey
	}
	if attempt, isTerminal := singleDispatchAttemptForWrite(call, classification, requested, receipt, singleDispatchBranches{
		providerRejected: CreateTicketBranchProviderRejected, defect: CreateTicketBranchDefect,
	}); isTerminal {
		return attempt
	}
	var created createdRequestResource
	if err := json.Unmarshal(result.response.body, &created); err != nil || !numericIDPattern.MatchString(created.IssueID) || !issueKeyPattern.MatchString(created.IssueKey) {
		return sdkgo.NewMutationUncertain(requested, newFailure(createTicketOperationID, sdkgo.FailureProtocol,
			"Jira Service Management accepted the customer request but returned an unusable request reference"), receipt)
	}
	output := requested
	output.IssueID, output.IssueKey, output.CreatedAt = created.IssueID, created.IssueKey, created.CreatedDate.timePointer()
	if created.CurrentStatus != nil {
		output.RequestStatus = &RequestStatus{Name: created.CurrentStatus.Status, Category: RequestStatusCategory(created.CurrentStatus.StatusCategory)}
	}
	receipt.ProviderObjectID = created.IssueKey
	return sdkgo.NewMutationBranch(CreateTicketBranchCreated, output, nil, receipt)
}

// buildCreateTicketRequest validates input and returns the request plus the echo used by every branch.
func buildCreateTicketRequest(input CreateTicketInput) (createTicketRequestBody, CreateTicketOutput, error) {
	requested := CreateTicketOutput{
		ServiceDeskID: strings.TrimSpace(input.ServiceDeskID), RequestTypeID: strings.TrimSpace(input.RequestTypeID),
		Summary: strings.TrimSpace(input.Summary), RaiseOnBehalfOfAccountID: strings.TrimSpace(input.RaiseOnBehalfOfAccountID),
	}
	if _, err := validateNumericID(requested.ServiceDeskID, "serviceDeskId"); err != nil {
		return createTicketRequestBody{}, requested, err
	}
	if _, err := validateNumericID(requested.RequestTypeID, "requestTypeId"); err != nil {
		return createTicketRequestBody{}, requested, err
	}
	summary, err := validateSingleLineText(input.Summary, "summary", maximumSummaryCharacters)
	if err != nil {
		return createTicketRequestBody{}, requested, err
	}
	if requested.RaiseOnBehalfOfAccountID != "" && !accountIDPattern.MatchString(requested.RaiseOnBehalfOfAccountID) {
		return createTicketRequestBody{}, requested, errors.New("raiseOnBehalfOfAccountId must be an Atlassian account ID")
	}
	if err := validatePlainText(input.Description, "description", false); err != nil {
		return createTicketRequestBody{}, requested, err
	}
	fieldValues, err := buildRequestFieldValues(summary, input.Description, input.AdditionalFieldValues)
	if err != nil {
		return createTicketRequestBody{}, requested, err
	}
	return createTicketRequestBody{
		ServiceDeskID: requested.ServiceDeskID, RequestTypeID: requested.RequestTypeID,
		RequestFieldValues: fieldValues, RaiseOnBehalfOf: requested.RaiseOnBehalfOfAccountID,
	}, requested, nil
}

func buildRequestFieldValues(summary string, description string, additional map[string]json.RawMessage) (map[string]json.RawMessage, error) {
	if len(additional) > MaxAdditionalFieldValues {
		return nil, fmt.Errorf("additionalFieldValues accepts at most %d fields", MaxAdditionalFieldValues)
	}
	values := map[string]json.RawMessage{}
	for fieldID, value := range additional {
		switch {
		case !jiraFieldIDPattern.MatchString(fieldID):
			return nil, errors.New("additionalFieldValues keys must be Jira field IDs such as customfield_10050")
		case fieldID == "summary" || fieldID == "description":
			return nil, errors.New("set summary and description with their own fields, not additionalFieldValues")
		case len(value) == 0 || len(value) > maximumFieldValueJSONBytes || !json.Valid(value):
			return nil, errors.New("each additionalFieldValues value must be valid JSON of at most 16 KiB")
		}
		values[fieldID] = value
	}
	encodedSummary, err := json.Marshal(summary)
	if err != nil {
		return nil, err
	}
	values["summary"] = encodedSummary
	if strings.TrimSpace(description) != "" {
		encodedDescription, err := json.Marshal(description)
		if err != nil {
			return nil, err
		}
		values["description"] = encodedDescription
	}
	return values, nil
}
