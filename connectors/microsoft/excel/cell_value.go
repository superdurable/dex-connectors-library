// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"strconv"
)

// excelTextPrefix is the apostrophe that makes Excel store typed input as literal text.
const excelTextPrefix = "'"

// CellValue is one Excel cell value as Microsoft Graph's values property holds
// it: text, a number, or a Boolean. Excel reads an empty cell as empty text, and
// a cell that holds an error as its error text, such as #N/A. A date reads as
// its serial number, the number of days since 1899-12-30; GetValues also
// returns the displayed text.
//
// Build values to write with TextCellValue, NumberCellValue, BooleanCellValue,
// and EmptyCellValue. Every write is literal: text is sent with Excel's leading
// apostrophe text prefix, so Excel stores it exactly as given instead of reading
// it as a formula, number, date, or Boolean, and does not keep the apostrophe in
// the value. A zero CellValue is absent: a write rejects it as defect, because
// Excel would leave the cell unchanged, so a forgotten value never goes unnoticed.
//
// A CellValue encodes as the plain JSON string, number, or Boolean, or null when
// it is absent, so Results stay readable in Dex Web.
type CellValue struct {
	value any
}

// TextCellValue returns a text value that Excel stores literally, including
// text that looks like a formula, such as =SUM(A1:A3), a number, such as 00123,
// or a date. Empty text clears the cell, like EmptyCellValue.
func TextCellValue(text string) CellValue { return CellValue{value: text} }

// NumberCellValue returns a number value. Excel stores numbers as IEEE 754
// doubles. A NaN or infinite number cannot be written and selects defect.
func NumberCellValue(number float64) CellValue { return CellValue{value: number} }

// BooleanCellValue returns a Boolean value, which Excel shows as TRUE or FALSE.
func BooleanCellValue(isTrue bool) CellValue { return CellValue{value: isTrue} }

// EmptyCellValue returns empty text, which clears the cell on write and is how
// Excel reads a cell without content.
func EmptyCellValue() CellValue { return CellValue{value: ""} }

// Text returns the value when it is text, including empty text, and false for any other kind.
func (value CellValue) Text() (string, bool) {
	text, isText := value.value.(string)
	return text, isText
}

// Number returns the value when it is a number, and false for any other kind.
func (value CellValue) Number() (float64, bool) {
	number, isNumber := value.value.(float64)
	return number, isNumber
}

// Boolean returns the value when it is a Boolean, and false for any other kind.
func (value CellValue) Boolean() (bool, bool) {
	isTrue, isBoolean := value.value.(bool)
	return isTrue, isBoolean
}

// IsEmpty reports whether the value is empty text, as Excel reads a cell without content.
func (value CellValue) IsEmpty() bool {
	text, isText := value.value.(string)
	return isText && text == ""
}

// IsAbsent reports whether the value is the zero CellValue, which holds no value at all.
func (value CellValue) IsAbsent() bool { return value.value == nil }

// String returns the text, the shortest decimal form of a number, TRUE or FALSE
// as Excel shows a Boolean, or an empty string for an absent value.
func (value CellValue) String() string {
	switch typed := value.value.(type) {
	case string:
		return typed
	case float64:
		return strconv.FormatFloat(typed, 'g', -1, 64)
	case bool:
		if typed {
			return "TRUE"
		}
		return "FALSE"
	default:
		return ""
	}
}

// MarshalJSON returns the JSON string, number, or Boolean, or null for an absent value.
func (value CellValue) MarshalJSON() ([]byte, error) {
	if value.value == nil {
		return []byte("null"), nil
	}
	return json.Marshal(value.value)
}

// UnmarshalJSON accepts one JSON string, number, Boolean, or null, which reads as absent.
func (value *CellValue) UnmarshalJSON(contents []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var decoded any
	if err := decoder.Decode(&decoded); err != nil {
		return err
	}
	switch typed := decoded.(type) {
	case nil:
		*value = CellValue{}
	case string:
		*value = TextCellValue(typed)
	case bool:
		*value = BooleanCellValue(typed)
	case json.Number:
		number, err := typed.Float64()
		if err != nil {
			return errors.New("Excel cell number is out of range")
		}
		*value = NumberCellValue(number)
	default:
		return errors.New("Excel cell value must be a string, number, Boolean, or null")
	}
	return nil
}

// wireValue returns the JSON value Graph's values property receives for a literal write.
func (value CellValue) wireValue() (any, error) {
	switch typed := value.value.(type) {
	case string:
		if typed == "" {
			return "", nil
		}
		return excelTextPrefix + typed, nil
	case float64:
		if math.IsNaN(typed) || math.IsInf(typed, 0) {
			return nil, errors.New("a number value is NaN or infinite")
		}
		return typed, nil
	case bool:
		return typed, nil
	default:
		return nil, errors.New("a value is absent; write EmptyCellValue to clear a cell")
	}
}

// isWritableKey reports whether the value can identify a row: non-empty text, a finite number, or a Boolean.
func (value CellValue) isWritableKey() bool {
	if value.IsEmpty() {
		return false
	}
	_, err := value.wireValue()
	return err == nil
}

// matchesStoredKey also accepts text stored with Excel's text prefix, so a kept apostrophe never hides an append.
func (value CellValue) matchesStoredKey(stored CellValue) bool {
	switch typed := value.value.(type) {
	case string:
		storedText, isText := stored.Text()
		return isText && (storedText == typed || storedText == excelTextPrefix+typed)
	case float64:
		storedNumber, isNumber := stored.Number()
		return isNumber && storedNumber == typed
	case bool:
		storedBoolean, isBoolean := stored.Boolean()
		return isBoolean && storedBoolean == typed
	default:
		return false
	}
}

// keyIdentity is a comparable form of a key value used to find duplicates in one request.
func (value CellValue) keyIdentity() any { return value.value }
