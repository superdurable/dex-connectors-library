// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// RecordUpdate sets fields on one existing record.
type RecordUpdate struct {
	// ID is the record ID, such as recXXXXXXXXXXXXXX.
	ID string `json:"id"`
	// Fields maps field names or IDs to their new cell values. Fields the
	// update does not name keep their values.
	Fields map[string]CellValue `json:"fields"`
}

// UpdateRecordsInput updates up to 10 records in one table by record ID.
//
// Each value replaces the field's whole value, including every link of a
// linked record field, so repeating the same update leaves the records
// unchanged.
type UpdateRecordsInput struct {
	// BaseID is the base ID, such as appXXXXXXXXXXXXXX.
	BaseID string `json:"baseId"`
	// TableIDOrName is the table ID, such as tblXXXXXXXXXXXXXX, or the table name.
	TableIDOrName string `json:"tableIdOrName"`
	// Records are the one to ten records to update, each named once.
	Records []RecordUpdate `json:"records"`
	// Typecast lets Airtable convert text to the field's type, such as adding a
	// new select option; false rejects a value of the wrong type.
	Typecast bool `json:"typecast,omitempty"`
}

// UpdatedRecords reports the records an update wrote.
type UpdatedRecords struct {
	// Records are the updated records with every non-empty field.
	Records []Record `json:"records"`
	// PartialSuccessReasons names why Airtable saved the records without every
	// attachment, such as attachmentsFailedUploading; empty when nothing failed.
	PartialSuccessReasons []string `json:"partialSuccessReasons,omitempty"`
}

// UpdateRecordsOperation implements the updateRecords connector Mutation.
type UpdateRecordsOperation struct{ client *Client }

type updateRecordsRequestBody struct {
	Records  []writeRecordRequest `json:"records"`
	Typecast bool                 `json:"typecast,omitempty"`
}

type updateRecordsResponseBody struct {
	Records *[]providerRecord     `json:"records"`
	Details *providerWriteDetails `json:"details"`
}

var updateRecordsBranches = operationBranches{
	notFound: UpdateRecordsBranchNotFound, rejected: UpdateRecordsBranchProviderRejected,
	invalidResponse: UpdateRecordsBranchInvalidResponse, defect: UpdateRecordsBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (UpdateRecordsOperation) Definition() sdkgo.MutationDefinition { return UpdateRecordsDefinition }

// IdempotencyKey derives the call's key from the stable connector call ID.
// Airtable accepts no idempotency key, so it is not sent; absolute field
// values make a repeated update converge instead.
func (UpdateRecordsOperation) IdempotencyKey(callID sdkgo.CallID, _ UpdateRecordsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one PATCH and classifies its attempt. A request that may have
// been applied is retried, because repeating absolute values is safe.
func (operation UpdateRecordsOperation) Invoke(call sdkgo.Call, input UpdateRecordsInput) sdkgo.MutationAttempt[UpdatedRecords] {
	requestBody, err := buildUpdateRecordsRequestBody(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateRecordsBranchDefect, UpdatedRecords{}, providerFailurePointer("updateRecords", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateRecordsBranchDefect, UpdatedRecords{}, providerFailurePointer("updateRecords", sdkgo.FailureLocalDefect, "updateRecords request could not be encoded"), sdkgo.Receipt{})
	}
	objectID := input.BaseID + "/" + input.TableIDOrName
	response, outcome := operation.client.exchange(call, "updateRecords", providerRequest{
		method: http.MethodPatch, path: recordPath(input.BaseID, input.TableIDOrName), baseID: input.BaseID, body: encoded,
	}, objectID)
	if outcome != nil {
		return mutationAttemptForOutcome[UpdatedRecords](*outcome, updateRecordsBranches)
	}
	receipt := operation.client.receipt(call, objectID)
	recordIDs := make([]string, 0, len(input.Records))
	for _, record := range input.Records {
		recordIDs = append(recordIDs, record.ID)
	}
	updated, err := decodeUpdatedRecords(response.body, recordIDs)
	if err != nil {
		return sdkgo.NewMutationBranch(UpdateRecordsBranchInvalidResponse, UpdatedRecords{}, providerFailurePointer("updateRecords", sdkgo.FailureProtocol, err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(UpdateRecordsBranchUpdated, updated, nil, receipt)
}

func buildUpdateRecordsRequestBody(input UpdateRecordsInput) (updateRecordsRequestBody, error) {
	if err := validateBaseAndTable(input.BaseID, input.TableIDOrName); err != nil {
		return updateRecordsRequestBody{}, err
	}
	if len(input.Records) < 1 || len(input.Records) > MaximumRecordsPerWrite {
		return updateRecordsRequestBody{}, fmt.Errorf("records must hold 1 to %d records", MaximumRecordsPerWrite)
	}
	body := updateRecordsRequestBody{Typecast: input.Typecast}
	seenRecordIDs := map[string]bool{}
	for index, record := range input.Records {
		label := fmt.Sprintf("records[%d]", index)
		if !recordIDPattern.MatchString(record.ID) {
			return updateRecordsRequestBody{}, fmt.Errorf("%s.id must be an Airtable record ID: rec followed by 14 letters and digits", label)
		}
		if seenRecordIDs[record.ID] {
			return updateRecordsRequestBody{}, fmt.Errorf("%s repeats record %s", label, record.ID)
		}
		seenRecordIDs[record.ID] = true
		if err := validateWrittenFields(label, record.Fields); err != nil {
			return updateRecordsRequestBody{}, err
		}
		body.Records = append(body.Records, writeRecordRequest{ID: record.ID, Fields: encodeWriteFields(record.Fields)})
	}
	return body, nil
}

func decodeUpdatedRecords(body []byte, requestedRecordIDs []string) (UpdatedRecords, error) {
	var decoded updateRecordsResponseBody
	if err := decodeResponse(body, &decoded); err != nil {
		return UpdatedRecords{}, err
	}
	if decoded.Records == nil {
		return UpdatedRecords{}, errors.New("Airtable returned an update response without records")
	}
	records, err := convertProviderRecords(*decoded.Records)
	if err != nil {
		return UpdatedRecords{}, err
	}
	if len(records) != len(requestedRecordIDs) {
		return UpdatedRecords{}, errors.New("Airtable returned a different number of records than the update wrote")
	}
	for _, record := range records {
		if !slices.Contains(requestedRecordIDs, record.ID) {
			return UpdatedRecords{}, errors.New("Airtable returned a record the update did not name")
		}
	}
	return UpdatedRecords{Records: records, PartialSuccessReasons: knownPartialSuccessReasons(decoded.Details)}, nil
}
