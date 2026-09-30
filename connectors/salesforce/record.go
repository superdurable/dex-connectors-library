// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
)

const (
	maxAPINameBytes = 255
	maxRecordFields = 500
	// aggregateResultType is the sObject type Salesforce reports for GROUP BY and aggregate rows.
	aggregateResultType = "AggregateResult"
)

var (
	// apiNamePattern accepts standard and custom object and field API names, such as Account or ns__Invoice__c.
	apiNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)
	// recordIDPattern accepts the 15-character case-sensitive and 18-character case-insensitive ID forms.
	recordIDPattern = regexp.MustCompile(`^[A-Za-z0-9]{15}(?:[A-Za-z0-9]{3})?$`)
)

// Record is one Salesforce record. Fields keeps each returned field's exact
// JSON value, so numbers, dates, and nested relationship records are never
// reinterpreted; a relationship value is an object with its own attributes.
type Record struct {
	// Type is the record's sObject API name, such as Contact, or AggregateResult for aggregate rows.
	Type string `json:"type"`
	// ID is the 18-character record ID when the record returned its Id field.
	ID string `json:"id,omitempty"`
	// Fields maps each returned field API name, including Id, to its JSON value.
	Fields map[string]json.RawMessage `json:"fields"`
}

// DecodeField decodes the named field into destination and reports whether the
// field was returned. Field names match exactly, or without case when no exact
// name matches, as Salesforce treats API names. A JSON null leaves destination
// unchanged.
func (record Record) DecodeField(fieldName string, destination any) (bool, error) {
	value, isPresent := record.fieldValue(fieldName)
	if !isPresent {
		return false, nil
	}
	if err := json.Unmarshal(value, destination); err != nil {
		return true, errors.New("Salesforce field " + fieldName + " cannot be decoded into the destination")
	}
	return true, nil
}

// StringField returns the named field's text and true when the field holds a
// JSON string. It returns false for an absent field, null, or another JSON type.
func (record Record) StringField(fieldName string) (string, bool) {
	value, isPresent := record.fieldValue(fieldName)
	if !isPresent || !bytes.HasPrefix(bytes.TrimSpace(value), []byte(`"`)) {
		return "", false
	}
	var text string
	if json.Unmarshal(value, &text) != nil {
		return "", false
	}
	return text, true
}

func (record Record) fieldValue(fieldName string) (json.RawMessage, bool) {
	if value, isPresent := record.Fields[fieldName]; isPresent {
		return value, true
	}
	for name, value := range record.Fields {
		if strings.EqualFold(name, fieldName) {
			return value, true
		}
	}
	return nil, false
}

// decodeRecord converts one untrusted Salesforce record object into a Record.
func decodeRecord(content json.RawMessage) (Record, error) {
	var members map[string]json.RawMessage
	if err := json.Unmarshal(content, &members); err != nil || members == nil {
		return Record{}, errors.New("record is not a JSON object")
	}
	var attributes struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(members["attributes"], &attributes); err != nil ||
		(!isAPIName(attributes.Type) && attributes.Type != aggregateResultType) {
		return Record{}, errors.New("record attributes lack a valid sObject type")
	}
	delete(members, "attributes")
	record := Record{Type: attributes.Type, Fields: members}
	if rawID, isPresent := members["Id"]; isPresent && !bytes.Equal(bytes.TrimSpace(rawID), []byte("null")) {
		if err := json.Unmarshal(rawID, &record.ID); err != nil || !isRecordID(record.ID) {
			return Record{}, errors.New("record Id is invalid")
		}
	}
	return record, nil
}

// encodeRecordFields validates caller field values and encodes the JSON write
// body. Id, attributes, and every excluded field cannot be written.
func encodeRecordFields(fields map[string]json.RawMessage, excludedFields ...string) ([]byte, error) {
	if len(fields) > maxRecordFields {
		return nil, errors.New("fields must hold at most 500 field values")
	}
	seenFields := make(map[string]bool, len(fields))
	for name, value := range fields {
		if !isFieldAPIName(name) {
			return nil, errors.New("fields keys must be field API names such as LastName")
		}
		lowerName := strings.ToLower(name)
		if lowerName == "id" || lowerName == "attributes" || seenFields[lowerName] {
			return nil, errors.New("fields must not set Id or attributes or repeat a field API name")
		}
		for _, excludedField := range excludedFields {
			if strings.EqualFold(name, excludedField) {
				return nil, errors.New("fields must not set " + excludedField + ", which the request path already names")
			}
		}
		seenFields[lowerName] = true
		if len(value) == 0 || !json.Valid(value) {
			return nil, errors.New("field " + name + " must hold one JSON value")
		}
	}
	if fields == nil {
		fields = map[string]json.RawMessage{}
	}
	body, err := json.Marshal(fields)
	if err != nil {
		return nil, errors.New("fields could not be encoded")
	}
	if len(body) > maxRequestBodyBytes {
		return nil, errors.New("fields exceed the 1 MiB request limit")
	}
	return body, nil
}

func isAPIName(value string) bool {
	return len(value) <= maxAPINameBytes && apiNamePattern.MatchString(value) && !strings.HasSuffix(value, "_")
}

func isFieldAPIName(value string) bool { return isAPIName(value) }

func isRecordID(value string) bool { return recordIDPattern.MatchString(value) }

// isSameRecordID compares IDs by their case-sensitive 15-character prefix, which both ID forms share.
func isSameRecordID(left string, right string) bool {
	return isRecordID(left) && isRecordID(right) && left[:15] == right[:15]
}
