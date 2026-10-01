// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// MaximumRecordsPerWrite is the most records one upsert or update sends;
	// write more in separate Steps.
	MaximumRecordsPerWrite = 10
	// MaximumFieldsPerRecord bounds the field values one written record carries.
	MaximumFieldsPerRecord = 500
	// maximumIdentifierBytes bounds a table, view, or field name or ID.
	maximumIdentifierBytes = 1000
)

var (
	baseIDPattern   = regexp.MustCompile(`^app[0-9A-Za-z]{14}$`)
	recordIDPattern = regexp.MustCompile(`^rec[0-9A-Za-z]{14}$`)
	// partialSuccessReasons are the reasons Airtable documents for a write that saved records but not every attachment.
	partialSuccessReasons = []string{"attachmentsFailedUploading", "attachmentUploadRateIsTooHigh"}
)

// Record is one Airtable record as Airtable returned it.
type Record struct {
	// ID is the record ID, such as recXXXXXXXXXXXXXX.
	ID string `json:"id"`
	// CreatedTime is when Airtable created the record, in UTC.
	CreatedTime time.Time `json:"createdTime"`
	// Fields maps each non-empty field, by field name, to its cell value.
	// Airtable omits empty fields, so a missing key means the field is empty.
	Fields map[string]CellValue `json:"fields"`
}

// providerRecord is one record in Airtable's response JSON.
type providerRecord struct {
	ID          string               `json:"id"`
	CreatedTime string               `json:"createdTime"`
	Fields      map[string]CellValue `json:"fields"`
}

// providerWriteDetails is the details object of a write that only partly succeeded.
type providerWriteDetails struct {
	Message string   `json:"message"`
	Reasons []string `json:"reasons"`
}

// convertProviderRecord validates one provider record and converts it to a Record.
func convertProviderRecord(record providerRecord) (Record, error) {
	if !recordIDPattern.MatchString(record.ID) {
		return Record{}, errors.New("Airtable returned a record without a valid record ID")
	}
	createdTime, err := time.Parse(time.RFC3339Nano, record.CreatedTime)
	if err != nil {
		return Record{}, errors.New("Airtable returned a record without a valid createdTime")
	}
	fields := record.Fields
	if fields == nil {
		fields = map[string]CellValue{}
	}
	return Record{ID: record.ID, CreatedTime: createdTime.UTC(), Fields: fields}, nil
}

func convertProviderRecords(records []providerRecord) ([]Record, error) {
	converted := make([]Record, 0, len(records))
	for _, record := range records {
		result, err := convertProviderRecord(record)
		if err != nil {
			return nil, err
		}
		converted = append(converted, result)
	}
	return converted, nil
}

// knownPartialSuccessReasons keeps the documented reasons of a partial success and drops any other text.
func knownPartialSuccessReasons(details *providerWriteDetails) []string {
	if details == nil || details.Message != "partialSuccess" {
		return nil
	}
	var reasons []string
	for _, reason := range details.Reasons {
		if slices.Contains(partialSuccessReasons, reason) && !slices.Contains(reasons, reason) {
			reasons = append(reasons, reason)
		}
	}
	return reasons
}

func validateBaseAndTable(baseID string, tableIDOrName string) error {
	if !baseIDPattern.MatchString(baseID) {
		return errors.New("baseId must be an Airtable base ID: app followed by 14 letters and digits")
	}
	if err := validateIdentifier("tableIdOrName", tableIDOrName); err != nil {
		return err
	}
	return nil
}

// validateIdentifier checks a table, view, or field name or ID that Airtable matches literally.
func validateIdentifier(label string, value string) error {
	if value == "" {
		return fmt.Errorf("%s is required", label)
	}
	if len(value) > maximumIdentifierBytes || !utf8.ValidString(value) {
		return fmt.Errorf("%s must be valid UTF-8 of at most %d bytes", label, maximumIdentifierBytes)
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("%s cannot contain control characters", label)
		}
	}
	return nil
}

// validateWrittenFields checks one record's field values before a write.
func validateWrittenFields(recordLabel string, fields map[string]CellValue) error {
	if len(fields) == 0 {
		return fmt.Errorf("%s needs at least one field value", recordLabel)
	}
	if len(fields) > MaximumFieldsPerRecord {
		return fmt.Errorf("%s can set at most %d fields", recordLabel, MaximumFieldsPerRecord)
	}
	for _, field := range sortedFieldNames(fields) {
		if err := validateIdentifier("field name", field); err != nil {
			return fmt.Errorf("%s: %w", recordLabel, err)
		}
		if !fields[field].isValidJSON() {
			return fmt.Errorf("%s: field %q holds no valid JSON cell value; use a CellValue constructor, or NullCellValue to clear it", recordLabel, field)
		}
	}
	return nil
}

func sortedFieldNames(fields map[string]CellValue) []string {
	names := make([]string, 0, len(fields))
	for name := range fields {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// encodeWriteFields copies validated field values for a request body.
func encodeWriteFields(fields map[string]CellValue) map[string]json.RawMessage {
	encoded := make(map[string]json.RawMessage, len(fields))
	for name, value := range fields {
		encoded[name] = json.RawMessage(value)
	}
	return encoded
}
