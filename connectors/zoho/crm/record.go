// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Zoho CRM module API names the examples use. Any other module, including a custom module, is
// named by its API name from Setup > Developer Hub > APIs and SDKs > API Names.
const (
	// ModuleLeads is the Leads module.
	ModuleLeads = "Leads"
	// ModuleContacts is the Contacts module; Email is its system duplicate-check field.
	ModuleContacts = "Contacts"
	// ModuleAccounts is the Accounts module; Account_Name is its system duplicate-check field.
	ModuleAccounts = "Accounts"
	// ModuleDeals is the Deals module; Deal_Name is its system duplicate-check field.
	ModuleDeals = "Deals"
)

// Zoho CRM field API names of the standard modules that the examples use. Zoho CRM defines them;
// the connector passes them and their values through unchanged.
const (
	// FieldModifiedTime is every record's last modification time.
	FieldModifiedTime = "Modified_Time"
	// FieldCreatedTime is every record's creation time.
	FieldCreatedTime = "Created_Time"
	// FieldOwner is a record's owner, a user lookup written as {"id": "<user ID>"}.
	FieldOwner = "Owner"
	// FieldEmail is a lead's or contact's primary email address.
	FieldEmail = "Email"
	// FieldFirstName is a lead's or contact's first name.
	FieldFirstName = "First_Name"
	// FieldLastName is a lead's or contact's last name, which Zoho CRM requires.
	FieldLastName = "Last_Name"
	// FieldAccountName is an account's name in Accounts, and the account lookup in Contacts and Deals.
	FieldAccountName = "Account_Name"
	// FieldContactName is a deal's contact lookup.
	FieldContactName = "Contact_Name"
	// FieldDealName is a deal's name.
	FieldDealName = "Deal_Name"
	// FieldStage is a deal's stage, a picklist whose values listModuleFields returns.
	FieldStage = "Stage"
	// FieldAmount is a deal's amount.
	FieldAmount = "Amount"
)

const (
	// MaxFieldsPerRequest is the most field API names a read selects, Zoho CRM's limit for Get Records.
	MaxFieldsPerRequest = 50
	// MaxFieldsPerWrite is the most fields one upsert or update sets.
	MaxFieldsPerWrite = 200
	zohoTimeLayout    = "2006-01-02T15:04:05-07:00"
)

var (
	moduleAPINamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,99}$`)
	fieldAPINamePattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,99}$`)
	// fieldPathPattern allows a lookup path of at most two joins, Zoho CRM's COQL limit.
	fieldPathPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,99}(?:\.[A-Za-z][A-Za-z0-9_]{0,99}){0,2}$`)
	recordIDPattern  = regexp.MustCompile(`^[0-9]{1,20}$`)
)

// Record is one Zoho CRM record. Fields holds each returned field's exact JSON value by API name:
// text, numbers, booleans, null, lists, and lookups such as {"name": "Zylker", "id": "4150868000000376008"}.
// Zoho's own keys that start with $, such as $approval, are dropped.
type Record struct {
	// Module is the module API name, such as Contacts.
	Module string `json:"module"`
	// ID is the record ID, a decimal string such as 4150868000000376008.
	ID string `json:"id"`
	// Fields holds every returned field except id.
	Fields map[string]json.RawMessage `json:"fields"`
}

// StringField returns a text field's value, or false when the field is absent, null, or not text.
func (record Record) StringField(fieldName string) (string, bool) {
	var value string
	isFound, err := record.DecodeField(fieldName, &value)
	if err != nil || !isFound {
		return "", false
	}
	return value, true
}

// LookupID returns the record or user ID of a lookup field such as Account_Name or Owner, or false
// when the field is absent, null, or not a lookup.
func (record Record) LookupID(fieldName string) (string, bool) {
	var lookup struct {
		ID string `json:"id"`
	}
	isFound, err := record.DecodeField(fieldName, &lookup)
	if err != nil || !isFound || !isRecordID(lookup.ID) {
		return "", false
	}
	return lookup.ID, true
}

// ModifiedAt returns Modified_Time, or false when it was not selected or is not a Zoho CRM time.
func (record Record) ModifiedAt() (time.Time, bool) {
	text, isFound := record.StringField(FieldModifiedTime)
	if !isFound {
		return time.Time{}, false
	}
	modifiedAt, err := time.Parse(time.RFC3339, text)
	return modifiedAt, err == nil
}

// DecodeField decodes one field into destination. It returns false without an error when the field
// is absent or null, and an error when the value does not fit destination.
func (record Record) DecodeField(fieldName string, destination any) (bool, error) {
	value, isFound := record.Fields[fieldName]
	if !isFound || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
		return false, nil
	}
	if err := json.Unmarshal(value, destination); err != nil {
		return true, fmt.Errorf("field %s: %w", fieldName, err)
	}
	return true, nil
}

// TextFieldValue encodes text as a field value for upsertRecord and updateRecord.
func TextFieldValue(text string) json.RawMessage {
	encoded, err := json.Marshal(text)
	if err != nil {
		panic(fmt.Sprintf("encoding a string cannot fail: %v", err))
	}
	return encoded
}

// LookupFieldValue encodes a lookup field value, such as a contact's Account_Name or a record's
// Owner, that points at the record or user with recordID.
func LookupFieldValue(recordID string) json.RawMessage {
	return json.RawMessage(`{"id":` + string(TextFieldValue(recordID)) + `}`)
}

// decodeRecord reads one record object; a record without a numeric id is invalid.
func decodeRecord(module string, raw json.RawMessage) (Record, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return Record{}, errors.New("a record is not a JSON object")
	}
	var recordID string
	if err := json.Unmarshal(fields["id"], &recordID); err != nil || !isRecordID(recordID) {
		return Record{}, errors.New("a record has no numeric id")
	}
	record := Record{Module: module, ID: recordID, Fields: make(map[string]json.RawMessage, len(fields))}
	for name, value := range fields {
		if name == "id" || !isFieldPath(name) {
			continue
		}
		record.Fields[name] = value
	}
	return record, nil
}

// decodeRecordPage reads {"data": [...], "info": {"more_records": ...}}; an empty body is an empty page.
func decodeRecordPage(module string, body []byte) ([]Record, bool, error) {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil, false, nil
	}
	var page struct {
		Data []json.RawMessage `json:"data"`
		Info struct {
			MoreRecords bool `json:"more_records"`
		} `json:"info"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return nil, false, errors.New("the page is not a JSON object with a data list")
	}
	records := make([]Record, 0, len(page.Data))
	for _, raw := range page.Data {
		record, err := decodeRecord(module, raw)
		if err != nil {
			return nil, false, err
		}
		records = append(records, record)
	}
	return records, page.Info.MoreRecords, nil
}

// validateWriteFields accepts 1 to MaxFieldsPerWrite fields with valid JSON values, never id or a $ key.
func validateWriteFields(fields map[string]json.RawMessage) error {
	if len(fields) == 0 || len(fields) > MaxFieldsPerWrite {
		return fmt.Errorf("fields must set 1 to %d fields", MaxFieldsPerWrite)
	}
	for name, value := range fields {
		if !isFieldAPIName(name) || strings.EqualFold(name, "id") {
			return fmt.Errorf("field name %q must be a Zoho CRM field API name other than id", name)
		}
		if !json.Valid(value) {
			return fmt.Errorf("field %s must hold one JSON value", name)
		}
	}
	return nil
}

// validateFieldSelection accepts up to MaxFieldsPerRequest distinct field paths; minimum is 0 or 1.
func validateFieldSelection(fieldNames []string, minimum int) error {
	if len(fieldNames) < minimum || len(fieldNames) > MaxFieldsPerRequest {
		return fmt.Errorf("fields must name %d to %d field API names", minimum, MaxFieldsPerRequest)
	}
	seen := make(map[string]bool, len(fieldNames))
	for _, name := range fieldNames {
		if !isFieldPath(name) || seen[name] {
			return fmt.Errorf("field %q must be a distinct Zoho CRM field API name such as Last_Name or Account_Name.Account_Name", name)
		}
		seen[name] = true
	}
	return nil
}

func validateModule(module string) error {
	if !moduleAPINamePattern.MatchString(module) {
		return errors.New("module must be a Zoho CRM module API name such as Contacts, Accounts, or Deals")
	}
	return nil
}

func validateRecordID(recordID string) error {
	if !isRecordID(recordID) {
		return errors.New("recordId must be a numeric Zoho CRM record ID such as 4150868000000376008")
	}
	return nil
}

func isRecordID(value string) bool { return recordIDPattern.MatchString(value) }

func isFieldAPIName(value string) bool { return fieldAPINamePattern.MatchString(value) }

func isFieldPath(value string) bool { return fieldPathPattern.MatchString(value) }

// formatZohoTime renders an instant as Zoho CRM's ISO 8601 form with an explicit UTC offset.
func formatZohoTime(instant time.Time) string {
	return instant.UTC().Format(zohoTimeLayout)
}
