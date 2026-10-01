// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"errors"
	"net/http"
	"net/url"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

// GetRecordInput identifies one record.
type GetRecordInput struct {
	// BaseID is the base ID, such as appXXXXXXXXXXXXXX.
	BaseID string `json:"baseId"`
	// TableIDOrName is the table ID, such as tblXXXXXXXXXXXXXX, or the table name.
	TableIDOrName string `json:"tableIdOrName"`
	// RecordID is the record ID, such as recXXXXXXXXXXXXXX.
	RecordID string `json:"recordId"`
}

// GetRecordOperation implements the getRecord connector Query.
//
// Airtable falls back to a base-wide lookup when the record is not in the
// named table, so a record ID from another table of the same base is still
// found; the response does not name the record's table.
type GetRecordOperation struct{ client *Client }

var getRecordBranches = operationBranches{
	notFound: GetRecordBranchNotFound, rejected: GetRecordBranchProviderRejected,
	invalidResponse: GetRecordBranchInvalidResponse, defect: GetRecordBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (GetRecordOperation) Definition() sdkgo.QueryDefinition { return GetRecordDefinition }

// Invoke sends one record read and classifies its attempt.
func (operation GetRecordOperation) Invoke(call sdkgo.Call, input GetRecordInput) sdkgo.QueryAttempt[Record] {
	if err := validateBaseAndTable(input.BaseID, input.TableIDOrName); err != nil {
		return sdkgo.NewQueryBranch(GetRecordBranchDefect, Record{}, providerFailurePointer("getRecord", sdkgo.FailureValidation, err.Error()), sdkgo.Receipt{})
	}
	if !recordIDPattern.MatchString(input.RecordID) {
		return sdkgo.NewQueryBranch(GetRecordBranchDefect, Record{}, providerFailurePointer("getRecord", sdkgo.FailureValidation, "recordId must be an Airtable record ID: rec followed by 14 letters and digits"), sdkgo.Receipt{})
	}
	response, outcome := operation.client.exchange(call, "getRecord", providerRequest{
		method: http.MethodGet, path: recordPath(input.BaseID, input.TableIDOrName) + "/" + url.PathEscape(input.RecordID),
		baseID: input.BaseID,
	}, input.RecordID)
	if outcome != nil {
		return queryAttemptForOutcome[Record](*outcome, getRecordBranches)
	}
	receipt := operation.client.receipt(call, input.RecordID)
	record, err := decodeRecord(response.body, input.RecordID)
	if err != nil {
		return sdkgo.NewQueryBranch(GetRecordBranchInvalidResponse, Record{}, providerFailurePointer("getRecord", sdkgo.FailureProtocol, err.Error()), receipt)
	}
	return sdkgo.NewQueryBranch(GetRecordBranchFound, record, nil, receipt)
}

func decodeRecord(body []byte, recordID string) (Record, error) {
	var decoded providerRecord
	if err := decodeResponse(body, &decoded); err != nil {
		return Record{}, err
	}
	record, err := convertProviderRecord(decoded)
	if err != nil {
		return Record{}, err
	}
	if record.ID != recordID {
		return Record{}, errors.New("Airtable returned a different record than requested")
	}
	return record, nil
}
