// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/superdurable/dex-connectors-library/sdkgo"
)

const (
	getRecordOperationID = "getRecord"
	maxGetRecordFields   = 200
)

// GetRecordInput identifies one record and the fields to read.
type GetRecordInput struct {
	// SObjectType is the record's object API name, such as Contact or Invoice__c.
	SObjectType string `json:"sObjectType"`
	// RecordID is the 15- or 18-character Salesforce record ID.
	RecordID string `json:"recordId"`
	// Fields lists from 1 to 200 field API names to read, such as Name and
	// Email. Relationship paths are not accepted; query them with queryRecords.
	Fields []string `json:"fields"`
}

// GetRecordOperation implements the getRecord connector operation.
type GetRecordOperation struct{ client *Client }

var getRecordBranches = operationBranches{
	operationID: getRecordOperationID, notFound: GetRecordBranchNotFound,
	requestRejected: GetRecordBranchProviderRejected, providerRejected: GetRecordBranchProviderRejected,
	invalidResponse: GetRecordBranchInvalidResponse, defect: GetRecordBranchDefect,
}

// Definition returns the immutable connector operation definition.
func (GetRecordOperation) Definition() sdkgo.QueryDefinition { return GetRecordDefinition }

// Invoke sends one sObject Rows read for the chosen fields and classifies its
// attempt. Invalid input selects defect without a provider request.
func (operation GetRecordOperation) Invoke(call sdkgo.Call, input GetRecordInput) sdkgo.QueryAttempt[Record] {
	client := operation.client
	if err := validateGetRecordInput(input); err != nil {
		return sdkgo.NewQueryBranch(GetRecordBranchDefect, Record{}, salesforceFailurePointer(sdkgo.FailureValidation, getRecordOperationID, err.Error()), sdkgo.Receipt{})
	}
	credential, outcome := client.resolveCredential(call, getRecordBranches)
	if outcome != nil {
		return queryAttemptFromOutcome[Record](client, call, getRecordBranches, outcome)
	}
	response, outcome := client.send(call, &credential, getRecordBranches, salesforceRequest{
		method: http.MethodGet, path: client.dataPath("sobjects", input.SObjectType, input.RecordID),
		query: url.Values{"fields": {strings.Join(input.Fields, ",")}}, responseLimit: client.maxResponseBytes,
	})
	if outcome != nil {
		return queryAttemptFromOutcome[Record](client, call, getRecordBranches, outcome)
	}
	receipt := client.receipt(call, input.RecordID, response.apiUsage)
	record, err := decodeRecord(response.body)
	if response.status != http.StatusOK || err != nil || !strings.EqualFold(record.Type, input.SObjectType) ||
		(record.ID != "" && !isSameRecordID(record.ID, input.RecordID)) {
		return sdkgo.NewQueryBranch(GetRecordBranchInvalidResponse, Record{}, salesforceFailurePointer(sdkgo.FailureProtocol, getRecordOperationID, "Salesforce returned an invalid record"), receipt)
	}
	if record.ID == "" {
		record.ID = input.RecordID
	}
	receipt.ProviderObjectID = record.ID
	return sdkgo.NewQueryBranch(GetRecordBranchFound, record, nil, receipt)
}

func validateGetRecordInput(input GetRecordInput) error {
	if !isAPIName(input.SObjectType) {
		return errors.New("sObjectType must be an object API name such as Contact")
	}
	if !isRecordID(input.RecordID) {
		return errors.New("recordId must be a 15- or 18-character Salesforce record ID")
	}
	if len(input.Fields) == 0 || len(input.Fields) > maxGetRecordFields {
		return fmt.Errorf("fields must list from 1 to %d field API names", maxGetRecordFields)
	}
	seenFields := make(map[string]bool, len(input.Fields))
	for _, field := range input.Fields {
		if !isFieldAPIName(field) {
			return errors.New("fields must be field API names such as Email, without relationship paths")
		}
		if seenFields[strings.ToLower(field)] {
			return errors.New("fields must not repeat a field API name")
		}
		seenFields[strings.ToLower(field)] = true
	}
	return nil
}
