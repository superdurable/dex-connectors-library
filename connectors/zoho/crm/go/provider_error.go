// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm

import (
	"encoding/json"
)

// ProviderError is one machine-readable Zoho CRM error about a record: its code and, when Zoho
// names one, the field API name and the record that already holds a unique value. It never
// carries Zoho CRM's message text, which can repeat record values.
type ProviderError struct {
	// Code is Zoho CRM's error code, such as MANDATORY_NOT_FOUND, INVALID_DATA, or DUPLICATE_DATA.
	Code string `json:"code"`
	// Field is the API name of the field Zoho CRM named, such as Last_Name, or empty.
	Field string `json:"field,omitempty"`
	// DuplicateRecordID is the record that already holds the value of a unique field, for DUPLICATE_DATA.
	DuplicateRecordID string `json:"duplicateRecordId,omitempty"`
}

// zohoErrorSummary holds only Zoho CRM's code and the identifiers in its details; message text is never read.
type zohoErrorSummary struct {
	code              string
	fieldAPIName      string
	duplicateRecordID string
	hasDetailsID      bool
}

type zohoErrorObject struct {
	Code    json.RawMessage `json:"code"`
	Status  json.RawMessage `json:"status"`
	Details struct {
		APIName         json.RawMessage `json:"api_name"`
		ID              json.RawMessage `json:"id"`
		DuplicateRecord struct {
			ID json.RawMessage `json:"id"`
		} `json:"duplicate_record"`
	} `json:"details"`
}

// describeZohoError reads a request-level error object, or the first record-level error in data[].
func describeZohoError(body []byte) zohoErrorSummary {
	var document struct {
		zohoErrorObject
		Data []json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &document) != nil {
		return zohoErrorSummary{}
	}
	if summary := summarizeErrorObject(document.zohoErrorObject); summary.code != "" {
		return summary
	}
	for _, item := range document.Data {
		if summary, isError := describeRecordError(item); isError {
			return summary
		}
	}
	return zohoErrorSummary{}
}

// describeRecordError reports false for a success entry or one that is not an error object.
func describeRecordError(item json.RawMessage) (zohoErrorSummary, bool) {
	var object zohoErrorObject
	if json.Unmarshal(item, &object) != nil {
		return zohoErrorSummary{}, false
	}
	var status string
	if json.Unmarshal(object.Status, &status) != nil || status != "error" {
		return zohoErrorSummary{}, false
	}
	return summarizeErrorObject(object), true
}

func summarizeErrorObject(object zohoErrorObject) zohoErrorSummary {
	summary := zohoErrorSummary{code: jsonStringMatching(object.Code, errorCodePattern.MatchString)}
	if summary.code == "" {
		return zohoErrorSummary{}
	}
	summary.fieldAPIName = jsonStringMatching(object.Details.APIName, isFieldPath)
	summary.duplicateRecordID = jsonStringMatching(object.Details.DuplicateRecord.ID, isRecordID)
	summary.hasDetailsID = len(object.Details.ID) != 0 && string(object.Details.ID) != "null"
	return summary
}

// isRecordIDInvalid reports INVALID_DATA about the id; a bad lookup names its own field instead.
func (summary zohoErrorSummary) isRecordIDInvalid() bool {
	if summary.code != errorCodeInvalidData {
		return false
	}
	return summary.fieldAPIName == "id" || (summary.fieldAPIName == "" && summary.hasDetailsID)
}

// providerErrors returns the summary as a public ProviderError list, or nil without a code.
func (summary zohoErrorSummary) providerErrors() []ProviderError {
	if summary.code == "" {
		return nil
	}
	return []ProviderError{{Code: summary.code, Field: summary.fieldAPIName, DuplicateRecordID: summary.duplicateRecordID}}
}

// describe appends the code, field, and duplicate record to a connector-written message.
func (summary zohoErrorSummary) describe(message string) string {
	details := joinNonEmpty([]string{summary.code, prefixedUnlessEmpty("field: ", summary.fieldAPIName),
		prefixedUnlessEmpty("duplicate record: ", summary.duplicateRecordID)}, "; ")
	if details == "" {
		return message
	}
	return message + " [" + details + "]"
}

func prefixedUnlessEmpty(prefix string, value string) string {
	if value == "" {
		return ""
	}
	return prefix + value
}

func jsonStringMatching(raw json.RawMessage, isAccepted func(string) bool) string {
	var value string
	if json.Unmarshal(raw, &value) != nil || !isAccepted(value) {
		return ""
	}
	return value
}
