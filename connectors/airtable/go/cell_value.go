// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"bytes"
	"encoding/json"
	"math"
)

// CellValue is one Airtable cell value in Airtable's JSON cell format, such as
// a string, a number, true, or an array of linked record IDs.
//
// Write values with the typed constructors: TextCellValue, NumberCellValue,
// CheckboxCellValue, LinkedRecordsCellValue, MultipleSelectsCellValue, and
// NullCellValue. A field type without a constructor, such as attachments or a
// collaborator, takes the raw JSON Airtable documents for it, for example
// CellValue(`[{"url":"https://example.com/receipt.pdf"}]`). A written value
// that is not valid JSON selects defect before any request is sent. The zero
// CellValue reads as JSON null, but a write rejects it as defect, so a
// forgotten value never clears a field; clear one on purpose with
// NullCellValue.
//
// Read values with Text, Number, Checkbox, and StringList. Airtable omits
// empty fields from every record it returns, so a missing map key means the
// field is empty: blank text, zero links, or an unchecked checkbox.
type CellValue []byte

// TextCellValue returns a text value for single line text, long text, email,
// URL, phone number, rich text, a single select option name, a date written
// as 2026-09-30, or a date-time in ISO 8601 form such as
// 2026-09-30T14:00:00.000Z.
func TextCellValue(text string) CellValue {
	return mustEncodeCellValue(text)
}

// NumberCellValue returns a number value for number, currency, percent, where
// 0.5 means 50 percent, rating, and duration, in seconds, fields. A NaN or
// infinite number cannot be written and selects defect.
func NumberCellValue(number float64) CellValue {
	if math.IsNaN(number) || math.IsInf(number, 0) {
		return CellValue("NaN")
	}
	return mustEncodeCellValue(number)
}

// CheckboxCellValue returns a checkbox value. Airtable reads an unchecked box
// back as an empty field.
func CheckboxCellValue(isChecked bool) CellValue {
	return mustEncodeCellValue(isChecked)
}

// LinkedRecordsCellValue returns a linked record field value that links
// exactly the given records, each a record ID such as recXXXXXXXXXXXXXX from
// the linked table. A write replaces every existing link, so no IDs unlinks
// all records.
func LinkedRecordsCellValue(recordIDs ...string) CellValue {
	return mustEncodeCellValue(nonNilStrings(recordIDs))
}

// MultipleSelectsCellValue returns a multiple select value that selects
// exactly the named options. A write replaces every existing selection. A new
// option name needs the write's Typecast flag, which lets Airtable add it.
func MultipleSelectsCellValue(optionNames ...string) CellValue {
	return mustEncodeCellValue(nonNilStrings(optionNames))
}

// NullCellValue returns JSON null, which clears the field on write.
func NullCellValue() CellValue {
	return CellValue("null")
}

// MarshalJSON returns the stored JSON, or null for the zero value.
func (value CellValue) MarshalJSON() ([]byte, error) {
	if len(value) == 0 {
		return []byte("null"), nil
	}
	return value, nil
}

// UnmarshalJSON stores a compact copy of one JSON value.
func (value *CellValue) UnmarshalJSON(contents []byte) error {
	var compacted bytes.Buffer
	if err := json.Compact(&compacted, contents); err != nil {
		return err
	}
	*value = CellValue(compacted.Bytes())
	return nil
}

// IsNull reports whether the value is JSON null, including the zero value.
func (value CellValue) IsNull() bool {
	return len(value) == 0 || bytes.Equal(bytes.TrimSpace(value), []byte("null"))
}

// Text returns a JSON string value, as Airtable returns text, select, date,
// and URL fields, and false for any other JSON type.
func (value CellValue) Text() (string, bool) {
	var text string
	if !value.decodesInto(&text) {
		return "", false
	}
	return text, true
}

// Number returns a JSON number value, as Airtable returns number, currency,
// percent, rating, duration, count, and autonumber fields, and false for any
// other JSON type.
func (value CellValue) Number() (float64, bool) {
	var number float64
	if !value.decodesInto(&number) {
		return 0, false
	}
	return number, true
}

// Checkbox reports whether the value is JSON true, the only value Airtable
// returns for a checked box.
func (value CellValue) Checkbox() bool {
	var isChecked bool
	return value.decodesInto(&isChecked) && isChecked
}

// StringList returns a JSON array of strings, as Airtable returns linked
// record IDs, multiple select option names, and text lookups, and false for
// any other JSON type.
func (value CellValue) StringList() ([]string, bool) {
	var stringList []string
	if !value.decodesInto(&stringList) || stringList == nil {
		return nil, false
	}
	return stringList, true
}

// isValidJSON reports whether a written value is present and holds one valid JSON value.
func (value CellValue) isValidJSON() bool {
	return len(value) > 0 && json.Valid(value)
}

// decodesInto decodes a non-null value into destination and reports success.
func (value CellValue) decodesInto(destination any) bool {
	return !value.IsNull() && json.Unmarshal(value, destination) == nil
}

func mustEncodeCellValue(value any) CellValue {
	encoded, err := json.Marshal(value)
	if err != nil {
		// Only strings, finite numbers, booleans, and string slices reach here; each always encodes.
		panic(err)
	}
	return CellValue(encoded)
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
