// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable

import (
	"math"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildFilterFormulaWritesEachTypedFilterSafely(t *testing.T) {
	for name, testCase := range map[string]struct {
		filters []FieldEqualityFilter
		raw     string
		formula string
	}{
		"nothing":                 {nil, "  ", ""},
		"one text filter":         {[]FieldEqualityFilter{TextFieldEquals("Policy Key", "refund-standard")}, "", `{Policy Key} = "refund-standard"`},
		"quote in text":           {[]FieldEqualityFilter{TextFieldEquals("Name", `O"Brien`)}, "", `{Name} = "O\"Brien"`},
		"formula characters stay": {[]FieldEqualityFilter{TextFieldEquals("Name", `x") , TRUE(), ("`)}, "", `{Name} = "x\") , TRUE(), (\""`},
		"field ID":                {[]FieldEqualityFilter{TextFieldEquals("fldPolicyKey00001", "a")}, "", `{fldPolicyKey00001} = "a"`},
		"integer":                 {[]FieldEqualityFilter{NumberFieldEquals("Limit", 250)}, "", `{Limit} = 250`},
		"large number":            {[]FieldEqualityFilter{NumberFieldEquals("Limit", 1e21)}, "", `{Limit} = 1000000000000000000000`},
		"negative fraction":       {[]FieldEqualityFilter{NumberFieldEquals("Delta", -0.125)}, "", `{Delta} = -0.125`},
		"checked":                 {[]FieldEqualityFilter{CheckboxFieldEquals("Active", true)}, "", `{Active} = TRUE()`},
		"unchecked":               {[]FieldEqualityFilter{CheckboxFieldEquals("Active", false)}, "", `NOT({Active})`},
		"raw formula only":        {nil, `{Amount} > 100`, `({Amount} > 100)`},
		"filters and raw formula": {[]FieldEqualityFilter{TextFieldEquals("A", "1"), CheckboxFieldEquals("B", true)}, `OR({C}, {D})`, `AND({A} = "1", {B} = TRUE(), (OR({C}, {D})))`},
	} {
		t.Run(name, func(t *testing.T) {
			formula, err := buildFilterFormula(testCase.filters, testCase.raw)
			require.NoError(t, err)
			require.Equal(t, testCase.formula, formula)
		})
	}
}

func TestBuildFilterFormulaRejectsWhatAirtableCannotEscape(t *testing.T) {
	text := "x"
	for name, filter := range map[string]FieldEqualityFilter{
		"opening brace in the field": TextFieldEquals("{Key", "x"),
		"closing brace in the field": TextFieldEquals("Key}", "x"),
		"backslash in the field":     TextFieldEquals(`Key\`, "x"),
		"blank field":                TextFieldEquals("", "x"),
		"line break in the field":    TextFieldEquals("Key\nName", "x"),
		"backslash in the text":      TextFieldEquals("Key", `C:\refunds`),
		"tab in the text":            TextFieldEquals("Key", "a\tb"),
		"NaN":                        NumberFieldEquals("Limit", math.NaN()),
		"negative infinity":          NumberFieldEquals("Limit", math.Inf(-1)),
		"no value":                   {Field: "Key"},
		"text and checkbox both set": {Field: "Key", Text: &text, Checkbox: new(bool)},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := buildFilterFormula([]FieldEqualityFilter{filter}, "")
			require.Error(t, err)
		})
	}
	_, err := buildFilterFormula([]FieldEqualityFilter{TextFieldEquals("Key", strings.Repeat("x", MaximumFormulaBytes))}, "")
	require.Error(t, err, "the combined formula is bounded too")
}
