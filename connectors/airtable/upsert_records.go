// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// MaximumMergeFields is Airtable's largest fieldsToMergeOn list.
const MaximumMergeFields = 3

// RecordUpsert is one record written by UpsertRecords.
type RecordUpsert struct {
	// Fields maps field names or IDs to cell values. It must hold a non-null
	// value for every merge field, under the same name or ID the merge list uses.
	Fields map[string]CellValue `json:"fields"`
}

// UpsertRecordsInput creates or updates up to 10 records in one table.
//
// Airtable matches each record to an existing record whose merge fields equal
// the record's values for them: no match creates a record, one match updates
// it, leaving fields the write does not name unchanged, and several matches
// reject the whole request. A repeated write of the same input therefore
// updates the records the first write created instead of adding duplicates.
type UpsertRecordsInput struct {
	// BaseID is the base ID, such as appXXXXXXXXXXXXXX.
	BaseID string `json:"baseId"`
	// TableIDOrName is the table ID, such as tblXXXXXXXXXXXXXX, or the table name.
	TableIDOrName string `json:"tableIdOrName"`
	// FieldsToMergeOn names one to three fields whose values identify a record,
	// such as a case ID. Airtable lists number, text, long text, single
	// select, multiple select, and date fields as merge fields, never a
	// computed field such as a formula, lookup, or rollup.
	FieldsToMergeOn []string `json:"fieldsToMergeOn"`
	// Records are the one to ten records to write; no two may share merge values.
	Records []RecordUpsert `json:"records"`
	// Typecast lets Airtable convert text to the field's type, such as adding a
	// new select option; false rejects a value of the wrong type.
	Typecast bool `json:"typecast,omitempty"`
}

// UpsertedRecords reports the records an upsert wrote.
type UpsertedRecords struct {
	// Records are the written records in input order, with every non-empty field.
	Records []Record `json:"records"`
	// CreatedRecordIDs are the records this answered request created. After a
	// retry whose earlier attempt created a record but lost the response, that
	// record appears in UpdatedRecordIDs instead.
	CreatedRecordIDs []string `json:"createdRecordIds"`
	// UpdatedRecordIDs are the existing records this answered request updated.
	UpdatedRecordIDs []string `json:"updatedRecordIds"`
	// PartialSuccessReasons names why Airtable saved the records without every
	// attachment, such as attachmentsFailedUploading; empty when nothing failed.
	PartialSuccessReasons []string `json:"partialSuccessReasons,omitempty"`
}

// UpsertRecordsOperation implements the upsertRecords connector Mutation.
type UpsertRecordsOperation struct{ client *Client }

type upsertRecordsRequestBody struct {
	PerformUpsert struct {
		FieldsToMergeOn []string `json:"fieldsToMergeOn"`
	} `json:"performUpsert"`
	Records  []writeRecordRequest `json:"records"`
	Typecast bool                 `json:"typecast,omitempty"`
}

// writeRecordRequest is one record in a PATCH body; ID is blank for an upsert.
type writeRecordRequest struct {
	ID     string                     `json:"id,omitempty"`
	Fields map[string]json.RawMessage `json:"fields"`
}

type upsertRecordsResponseBody struct {
	Records        *[]providerRecord     `json:"records"`
	CreatedRecords *[]string             `json:"createdRecords"`
	UpdatedRecords *[]string             `json:"updatedRecords"`
	Details        *providerWriteDetails `json:"details"`
}

var upsertRecordsBranches = operationBranches{
	rejected: UpsertRecordsBranchProviderRejected, invalidResponse: UpsertRecordsBranchInvalidResponse,
	defect: UpsertRecordsBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (UpsertRecordsOperation) Definition() sdkgo.MutationDefinition { return UpsertRecordsDefinition }

// IdempotencyKey derives the call's key from the stable connector call ID.
// Airtable accepts no idempotency key, so it is not sent; the merge fields
// make a repeated write converge instead.
func (UpsertRecordsOperation) IdempotencyKey(callID sdkgo.CallID, _ UpsertRecordsInput) sdkgo.IdempotencyKey {
	return sdkgo.IdempotencyKey(callID)
}

// Invoke sends one PATCH with performUpsert and classifies its attempt. A
// request that may have been applied is retried rather than reported
// uncertain, because the retry updates the records the first attempt created.
func (operation UpsertRecordsOperation) Invoke(call sdkgo.Call, input UpsertRecordsInput) sdkgo.MutationAttempt[UpsertedRecords] {
	requestBody, err := buildUpsertRecordsRequestBody(input)
	if err != nil {
		return sdkgo.NewMutationBranch(UpsertRecordsBranchDefect, UpsertedRecords{}, providerFailurePointer("upsertRecords", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	encoded, err := json.Marshal(requestBody)
	if err != nil {
		return sdkgo.NewMutationBranch(UpsertRecordsBranchDefect, UpsertedRecords{}, providerFailurePointer("upsertRecords", sdkgo.FailureLocalDefect, "upsertRecords request could not be encoded"), sdkgo.Receipt{})
	}
	objectID := input.BaseID + "/" + input.TableIDOrName
	response, outcome := operation.client.exchange(call, "upsertRecords", providerRequest{
		method: http.MethodPatch, path: recordPath(input.BaseID, input.TableIDOrName), baseID: input.BaseID, body: encoded,
	}, objectID)
	if outcome != nil {
		return mutationAttemptForOutcome[UpsertedRecords](*outcome, upsertRecordsBranches)
	}
	receipt := operation.client.receipt(call, objectID)
	upserted, err := decodeUpsertedRecords(response.body, len(input.Records))
	if err != nil {
		return sdkgo.NewMutationBranch(UpsertRecordsBranchInvalidResponse, UpsertedRecords{}, providerFailurePointer("upsertRecords", sdkgo.FailureProtocol, err.Error()), receipt)
	}
	return sdkgo.NewMutationBranch(UpsertRecordsBranchUpserted, upserted, nil, receipt)
}

func buildUpsertRecordsRequestBody(input UpsertRecordsInput) (upsertRecordsRequestBody, error) {
	if err := validateBaseAndTable(input.BaseID, input.TableIDOrName); err != nil {
		return upsertRecordsRequestBody{}, err
	}
	if len(input.FieldsToMergeOn) < 1 || len(input.FieldsToMergeOn) > MaximumMergeFields {
		return upsertRecordsRequestBody{}, fmt.Errorf("fieldsToMergeOn must name 1 to %d fields", MaximumMergeFields)
	}
	mergeFields, err := uniqueIdentifiers("fieldsToMergeOn", input.FieldsToMergeOn)
	if err != nil {
		return upsertRecordsRequestBody{}, err
	}
	if len(mergeFields) != len(input.FieldsToMergeOn) {
		return upsertRecordsRequestBody{}, errors.New("fieldsToMergeOn cannot repeat a field")
	}
	if len(input.Records) < 1 || len(input.Records) > MaximumRecordsPerWrite {
		return upsertRecordsRequestBody{}, fmt.Errorf("records must hold 1 to %d records", MaximumRecordsPerWrite)
	}
	var body upsertRecordsRequestBody
	body.PerformUpsert.FieldsToMergeOn = mergeFields
	body.Typecast = input.Typecast
	seenMergeKeys := map[string]int{}
	for index, record := range input.Records {
		label := fmt.Sprintf("records[%d]", index)
		if err := validateWrittenFields(label, record.Fields); err != nil {
			return upsertRecordsRequestBody{}, err
		}
		mergeKey, err := recordMergeKey(label, record.Fields, mergeFields)
		if err != nil {
			return upsertRecordsRequestBody{}, err
		}
		if earlier, exists := seenMergeKeys[mergeKey]; exists {
			return upsertRecordsRequestBody{}, fmt.Errorf("%s repeats the merge field values of records[%d]", label, earlier)
		}
		seenMergeKeys[mergeKey] = index
		body.Records = append(body.Records, writeRecordRequest{Fields: encodeWriteFields(record.Fields)})
	}
	return body, nil
}

// recordMergeKey joins the compact JSON of a record's merge field values, which Airtable uses to match it.
func recordMergeKey(label string, fields map[string]CellValue, mergeFields []string) (string, error) {
	parts := make([]string, 0, len(mergeFields))
	for _, mergeField := range mergeFields {
		value, exists := fields[mergeField]
		if !exists || value.IsNull() {
			return "", fmt.Errorf("%s needs a non-null value for merge field %q", label, mergeField)
		}
		var compacted bytes.Buffer
		if err := json.Compact(&compacted, value); err != nil {
			return "", fmt.Errorf("%s: merge field %q holds invalid JSON", label, mergeField)
		}
		parts = append(parts, compacted.String())
	}
	return strings.Join(parts, "\x00"), nil
}

func decodeUpsertedRecords(body []byte, requestedRecords int) (UpsertedRecords, error) {
	var decoded upsertRecordsResponseBody
	if err := decodeResponse(body, &decoded); err != nil {
		return UpsertedRecords{}, err
	}
	if decoded.Records == nil || decoded.CreatedRecords == nil || decoded.UpdatedRecords == nil {
		return UpsertedRecords{}, errors.New("Airtable returned an upsert response without records, createdRecords, and updatedRecords")
	}
	if len(*decoded.Records) != requestedRecords {
		return UpsertedRecords{}, errors.New("Airtable returned a different number of records than the upsert wrote")
	}
	records, err := convertProviderRecords(*decoded.Records)
	if err != nil {
		return UpsertedRecords{}, err
	}
	writtenIDs := make([]string, 0, len(records))
	for _, record := range records {
		writtenIDs = append(writtenIDs, record.ID)
	}
	created, updated := nonNilStrings(*decoded.CreatedRecords), nonNilStrings(*decoded.UpdatedRecords)
	for _, recordID := range append(slices.Clone(created), updated...) {
		if !slices.Contains(writtenIDs, recordID) {
			return UpsertedRecords{}, errors.New("Airtable listed a created or updated record that the upsert did not return")
		}
	}
	for _, recordID := range created {
		if slices.Contains(updated, recordID) {
			return UpsertedRecords{}, errors.New("Airtable listed one record as both created and updated")
		}
	}
	return UpsertedRecords{
		Records: records, CreatedRecordIDs: created, UpdatedRecordIDs: updated,
		PartialSuccessReasons: knownPartialSuccessReasons(decoded.Details),
	}, nil
}
