// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/singledispatch"
)

// CreateRecordInput appends one record to an Airtable table.
// An unknown result may be resent up to MaxSends and can produce duplicates.
type CreateRecordInput struct {
	// BaseID is the Airtable base ID.
	BaseID string `json:"baseId"`
	// TableIDOrName identifies the table, preferably by stable ID.
	TableIDOrName string `json:"tableIdOrName"`
	// Fields maps names or IDs to typed cell values.
	Fields map[string]CellValue `json:"fields"`
	// Typecast enables Airtable's best-effort conversion of text values.
	Typecast bool `json:"typecast,omitempty"`
}

// CreateRecordOperation implements the limited-repeat createRecord Mutation.
type CreateRecordOperation struct{ client *Client }

type createRecordDispatch struct {
	client   *Client
	call     sdkgo.Call
	request  providerRequest
	response providerResponse
	outcome  *providerOutcome
}

// Definition returns the immutable connector operation definition.
func (CreateRecordOperation) Definition() sdkgo.MutationDefinition { return CreateRecordDefinition }

// IdempotencyKey correlates attempts; Airtable does not accept this key.
func (CreateRecordOperation) IdempotencyKey(callID sdkgo.CallID, _ CreateRecordInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke appends a record with a bounded number of possibly effective sends.
func (operation CreateRecordOperation) Invoke(call sdkgo.Call, input CreateRecordInput) sdkgo.MutationAttempt[Record] {
	const operationID = "createRecord"
	if err := validateBaseAndTable(input.BaseID, input.TableIDOrName); err != nil {
		return sdkgo.NewMutationBranch(CreateRecordBranchDefect, Record{}, providerFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	if err := validateWrittenFields("record", input.Fields); err != nil {
		return sdkgo.NewMutationBranch(CreateRecordBranchDefect, Record{}, providerFailurePointer(operationID, sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	body, err := json.Marshal(struct {
		Fields   map[string]json.RawMessage `json:"fields"`
		Typecast bool                       `json:"typecast,omitempty"`
	}{Fields: encodeWriteFields(input.Fields), Typecast: input.Typecast})
	if err != nil {
		return sdkgo.NewMutationBranch(CreateRecordBranchDefect, Record{}, providerFailurePointer(operationID, sdkgo.FailureLocalDefect, "Airtable create request could not be encoded"), sdkgo.Receipt{})
	}
	fence, err := singledispatch.NewFence(call, CreateRecordDefinition.StepDefaults.ExecuteMethodTimeout, singledispatch.WithMaxSends(call.MaxSends()))
	if err != nil {
		return sdkgo.NewMutationBranch(CreateRecordBranchDefect, Record{}, providerFailurePointer(operationID, sdkgo.FailureLocalDefect, "Airtable create checkpoint could not be initialized"), sdkgo.Receipt{})
	}
	decision := fence.Decide()
	if decision.Kind == singledispatch.FailClosed {
		return sdkgo.NewMutationBranch(CreateRecordBranchDefect, Record{}, providerFailurePointer(operationID, sdkgo.FailureLocalDefect, "Airtable create checkpoint is invalid"), sdkgo.Receipt{})
	}
	if decision.Marker.ProviderID != "" {
		record, readOutcome := operation.readCreatedRecord(call, input, decision.Marker.ProviderID)
		if readOutcome == nil {
			return sdkgo.NewMutationBranch(CreateRecordBranchCreated, record, nil, operation.client.receipt(call, record.ID))
		}
		if readOutcome.disposition == outcomeRetry {
			return sdkgo.NewMutationRetry[Record](readOutcome.failure, readOutcome.retryAfter)
		}
		if readOutcome.disposition != outcomeNotFound {
			return sdkgo.NewMutationUncertain(Record{}, readOutcome.failure, operation.client.receipt(call, decision.Marker.ProviderID))
		}
	}
	if decision.Kind == singledispatch.ReadBackRequired {
		return sdkgo.NewMutationUncertain(Record{}, providerFailure(operationID, sdkgo.FailureTransport, "an earlier Airtable create may have succeeded and cannot be confirmed"), operation.client.receipt(call, decision.Marker.ProviderID))
	}
	dispatch := &createRecordDispatch{client: operation.client, call: call, request: providerRequest{
		method: http.MethodPost, path: recordPath(input.BaseID, input.TableIDOrName), baseID: input.BaseID, body: body,
	}}
	if err := fence.Dispatch(dispatch.execute); err != nil {
		return sdkgo.NewMutationRetry[Record](providerFailure(operationID, sdkgo.FailureAvailability, "Airtable create dispatch checkpoint is unavailable"), 0)
	}
	if dispatch.outcome != nil {
		outcome := dispatch.outcome
		switch outcome.disposition {
		case outcomeRetry:
			if outcome.failure.Kind == sdkgo.FailureRateLimit || dispatch.response.statusCode == http.StatusServiceUnavailable || outcome.receipt.CallID == "" {
				if err := fence.RecordCanResend(); err != nil {
					return sdkgo.NewMutationRetry[Record](providerFailure(operationID, sdkgo.FailureAvailability, "Airtable create retry checkpoint is unavailable"), 0)
				}
			}
			return sdkgo.NewMutationRetry[Record](outcome.failure, outcome.retryAfter)
		case outcomeDefect:
			return sdkgo.NewMutationBranch(CreateRecordBranchDefect, Record{}, &outcome.failure, outcome.receipt)
		case outcomeInvalidResponse:
			return sdkgo.NewMutationUncertain(Record{}, outcome.failure, outcome.receipt)
		default:
			if decision.MaximumPriorSends > 0 {
				return sdkgo.NewMutationUncertain(Record{}, providerFailure(operationID, sdkgo.FailureTransport, "an earlier Airtable create may have succeeded despite this rejection"), outcome.receipt)
			}
			return sdkgo.NewMutationBranch(CreateRecordBranchProviderRejected, Record{}, &outcome.failure, outcome.receipt)
		}
	}
	var provided providerRecord
	if err := decodeResponse(dispatch.response.body, &provided); err != nil {
		return sdkgo.NewMutationUncertain(Record{}, providerFailure(operationID, sdkgo.FailureProtocol, "Airtable accepted the create but returned an invalid record"), operation.client.receipt(call, ""))
	}
	record, err := convertProviderRecord(provided)
	if err != nil {
		return sdkgo.NewMutationUncertain(Record{}, providerFailure(operationID, sdkgo.FailureProtocol, "Airtable accepted the create but returned an invalid record"), operation.client.receipt(call, ""))
	}
	// A lost checkpoint leaves the accepted result known to this attempt.
	_ = fence.RecordProviderID(record.ID)
	return sdkgo.NewMutationBranch(CreateRecordBranchCreated, record, nil, operation.client.receipt(call, record.ID))
}

func (operation CreateRecordOperation) readCreatedRecord(call sdkgo.Call, input CreateRecordInput, recordID string) (Record, *providerOutcome) {
	response, outcome := operation.client.exchange(call, "createRecord", providerRequest{
		method: http.MethodGet, path: recordPath(input.BaseID, input.TableIDOrName) + "/" + url.PathEscape(recordID), baseID: input.BaseID,
	}, recordID)
	if outcome != nil {
		return Record{}, outcome
	}
	record, err := decodeRecord(response.body, recordID)
	if err != nil {
		return Record{}, operation.client.outcome(outcomeInvalidResponse, "createRecord", sdkgo.FailureProtocol, fmt.Sprintf("Airtable returned an invalid read-back for record %s", recordID), operation.client.receipt(call, recordID))
	}
	return record, nil
}

func (dispatch *createRecordDispatch) execute(ctx context.Context) error {
	dispatch.response, dispatch.outcome = dispatch.client.exchangeWithContext(ctx, dispatch.call, "createRecord", dispatch.request, "")
	return nil
}
