// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm

import (
	"bytes"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const getRecordOperation = "getRecord"

// GetRecordInput reads one record by ID.
type GetRecordInput struct {
	// Module is the module API name, such as Deals.
	Module string `json:"module"`
	// RecordID is the record ID, such as 4150868000000376008.
	RecordID string `json:"recordId"`
	// Fields are up to 50 field API names to return; empty returns every field of the record.
	Fields []string `json:"fields,omitempty"`
}

// GetRecordOperation is the getRecord Query.
type GetRecordOperation struct {
	client *Client
}

// Definition returns the immutable connector operation definition.
func (GetRecordOperation) Definition() sdkgo.QueryDefinition { return GetRecordDefinition }

// Invoke sends GET /crm/v8/{module}/{recordId}, with fields when Fields is set. A 204, an empty
// data list, or INVALID_DATA about the id selects notFound.
func (operation GetRecordOperation) Invoke(call sdkgo.Call, input GetRecordInput) sdkgo.QueryAttempt[Record] {
	branches := queryBranches{
		notFound: GetRecordBranchNotFound, providerRejected: GetRecordBranchProviderRejected,
		invalidResponse: GetRecordBranchInvalidResponse, defect: GetRecordBranchDefect,
	}
	if err := validateGetRecordInput(input); err != nil {
		return sdkgo.NewQueryBranch(GetRecordBranchDefect, Record{}, crmFailurePointer(sdkgo.FailureValidation, getRecordOperation, err.Error()), sdkgo.Receipt{})
	}
	session, cancel, failure := operation.client.startSession(call, getRecordOperation)
	if failure != nil {
		return queryAttemptForSession[Record](failure, branches)
	}
	defer cancel()
	request := crmRequest{method: http.MethodGet, path: recordPath(input.Module, input.RecordID)}
	if len(input.Fields) != 0 {
		request.query = url.Values{"fields": {strings.Join(input.Fields, ",")}}
	}
	result := operation.client.exchange(session, getRecordOperation, request)
	receipt := operation.client.receipt(call, result.response, input.RecordID)
	if attempt, isTerminal := queryAttemptForExchange[Record](result, receipt, branches); isTerminal {
		return attempt
	}
	if result.response.statusCode == http.StatusNoContent || len(bytes.TrimSpace(result.response.body)) == 0 {
		return sdkgo.NewQueryBranch(GetRecordBranchNotFound, Record{}, crmFailurePointer(sdkgo.FailureNotFound, getRecordOperation, "Zoho CRM found no record with the ID (HTTP 204)"), receipt)
	}
	records, _, err := decodeRecordPage(input.Module, result.response.body)
	switch {
	case err != nil:
		return sdkgo.NewQueryBranch(GetRecordBranchInvalidResponse, Record{}, crmFailurePointer(sdkgo.FailureProtocol, getRecordOperation, "Zoho CRM returned an invalid record: "+err.Error()), receipt)
	case len(records) == 0:
		return sdkgo.NewQueryBranch(GetRecordBranchNotFound, Record{}, crmFailurePointer(sdkgo.FailureNotFound, getRecordOperation, "Zoho CRM returned no record with the ID"), receipt)
	case len(records) != 1 || records[0].ID != input.RecordID:
		return sdkgo.NewQueryBranch(GetRecordBranchInvalidResponse, Record{}, crmFailurePointer(sdkgo.FailureProtocol, getRecordOperation, "Zoho CRM returned a record other than the one requested"), receipt)
	}
	return sdkgo.NewQueryBranch(GetRecordBranchFound, records[0], nil, receipt)
}

func validateGetRecordInput(input GetRecordInput) error {
	if err := validateModule(input.Module); err != nil {
		return err
	}
	if err := validateRecordID(input.RecordID); err != nil {
		return err
	}
	if err := validateFieldSelection(input.Fields, 0); err != nil {
		return err
	}
	for _, fieldName := range input.Fields {
		if !isFieldAPIName(fieldName) {
			return errors.New("getRecord fields must be field API names without a lookup path; use findRecords for Account_Name.Account_Name")
		}
	}
	return nil
}
