// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"unicode"
)

const (
	// MaximumFieldEqualityFilters bounds the typed filters of one listRecords call.
	MaximumFieldEqualityFilters = 25
	// MaximumFormulaBytes bounds the raw formula and the complete filterByFormula sent to Airtable.
	MaximumFormulaBytes = 16000
)

// FieldEqualityFilter matches records whose field equals one typed value.
// Exactly one of Text, Number, and Checkbox is set; prefer the constructors
// TextFieldEquals, NumberFieldEquals, and CheckboxFieldEquals.
//
// The connector writes the filter as an Airtable formula such as
// {Policy Key} = "refund-standard", so it follows Airtable's formula
// comparison rules. A linked record, lookup, or multiple select field compares
// its displayed text, such as the linked records' primary field values joined
// by commas.
type FieldEqualityFilter struct {
	// Field is the field name, or its field ID such as fldXXXXXXXXXXXXXX. A
	// name cannot contain a brace or a backslash, because Airtable documents no
	// escape for them inside a field reference; use the field ID instead.
	Field string `json:"field"`
	// Text matches a text, email, URL, phone, single select, or date field.
	// It cannot contain a backslash or a control character such as a line
	// break; use ListRecordsInput.Formula for such values.
	Text *string `json:"text,omitempty"`
	// Number matches a number, currency, percent, rating, duration, count, or
	// autonumber field. It must be finite.
	Number *float64 `json:"number,omitempty"`
	// Checkbox matches a checked box when true and an unchecked box when false.
	Checkbox *bool `json:"checkbox,omitempty"`
}

// TextFieldEquals returns a filter matching records whose field equals text.
func TextFieldEquals(field string, text string) FieldEqualityFilter {
	return FieldEqualityFilter{Field: field, Text: &text}
}

// NumberFieldEquals returns a filter matching records whose field equals number.
func NumberFieldEquals(field string, number float64) FieldEqualityFilter {
	return FieldEqualityFilter{Field: field, Number: &number}
}

// CheckboxFieldEquals returns a filter matching checked records when
// isChecked is true and unchecked records when it is false.
func CheckboxFieldEquals(field string, isChecked bool) FieldEqualityFilter {
	return FieldEqualityFilter{Field: field, Checkbox: &isChecked}
}

// buildFilterFormula joins the typed filters and the raw formula with AND.
// It returns an empty formula when there is neither.
func buildFilterFormula(filters []FieldEqualityFilter, rawFormula string) (string, error) {
	if len(filters) > MaximumFieldEqualityFilters {
		return "", fmt.Errorf("at most %d field filters are allowed", MaximumFieldEqualityFilters)
	}
	if len(rawFormula) > MaximumFormulaBytes {
		return "", fmt.Errorf("formula is limited to %d bytes", MaximumFormulaBytes)
	}
	conditions := make([]string, 0, len(filters)+1)
	for index, filter := range filters {
		condition, err := filter.formulaCondition()
		if err != nil {
			return "", fmt.Errorf("filters[%d]: %w", index, err)
		}
		conditions = append(conditions, condition)
	}
	if strings.TrimSpace(rawFormula) != "" {
		conditions = append(conditions, "("+rawFormula+")")
	}
	formula := ""
	switch len(conditions) {
	case 0:
	case 1:
		formula = conditions[0]
	default:
		formula = "AND(" + strings.Join(conditions, ", ") + ")"
	}
	if len(formula) > MaximumFormulaBytes {
		return "", fmt.Errorf("the combined filter formula is limited to %d bytes", MaximumFormulaBytes)
	}
	return formula, nil
}

func (filter FieldEqualityFilter) formulaCondition() (string, error) {
	reference, err := formulaFieldReference(filter.Field)
	if err != nil {
		return "", err
	}
	setValues := 0
	for _, isSet := range []bool{filter.Text != nil, filter.Number != nil, filter.Checkbox != nil} {
		if isSet {
			setValues++
		}
	}
	if setValues != 1 {
		return "", errors.New("exactly one of text, number, and checkbox must be set")
	}
	switch {
	case filter.Text != nil:
		literal, err := formulaTextLiteral(*filter.Text)
		if err != nil {
			return "", err
		}
		return reference + " = " + literal, nil
	case filter.Number != nil:
		if math.IsNaN(*filter.Number) || math.IsInf(*filter.Number, 0) {
			return "", errors.New("number must be finite")
		}
		return reference + " = " + strconv.FormatFloat(*filter.Number, 'f', -1, 64), nil
	case *filter.Checkbox:
		return reference + " = TRUE()", nil
	default:
		// An unchecked box is empty when read, so NOT covers both empty and false.
		return "NOT(" + reference + ")", nil
	}
}

// formulaFieldReference wraps a field name or ID in braces, rejecting characters Airtable documents no escape for.
func formulaFieldReference(field string) (string, error) {
	if err := validateIdentifier("field", field); err != nil {
		return "", err
	}
	if strings.ContainsAny(field, `{}\`) {
		return "", errors.New("field name cannot contain {, }, or a backslash; use the field ID")
	}
	return "{" + field + "}", nil
}

// formulaTextLiteral quotes text, escaping double quotes with a backslash as Airtable documents.
func formulaTextLiteral(text string) (string, error) {
	if strings.Contains(text, `\`) {
		return "", errors.New("text cannot contain a backslash; use formula")
	}
	for _, character := range text {
		if unicode.IsControl(character) {
			return "", errors.New("text cannot contain control characters such as line breaks; use formula")
		}
	}
	return `"` + strings.ReplaceAll(text, `"`, `\"`) + `"`, nil
}
