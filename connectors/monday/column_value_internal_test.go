// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestColumnValuesEncodeMondaysDocumentedJSON pins each type to the write example on its monday.com reference page.
func TestColumnValuesEncodeMondaysDocumentedJSON(t *testing.T) {
	encoded, err := encodeColumnValues(map[string]ColumnValue{
		"text":      TextValue("Sample text"),
		"long_text": LongTextValue("Line one\nLine two"),
		"numbers":   NumberValue(42.5),
		"status":    StatusLabelValue("Done"),
		"status2":   StatusIndexValue(1),
		"date":      DateTimeValue("2026-06-15", "09:00:00"),
		"people":    {Type: ColumnTypePeople, PersonIDs: []int64{48202303}, TeamIDs: []int64{51166}},
		"checkbox":  CheckboxValue(true),
		"unchecked": CheckboxValue(false),
		"email":     {Type: ColumnTypeEmail, Email: "contact@example.com", Text: "Main Contact"},
		"email2":    EmailValue("jane@example.com"),
		"link":      LinkValue("https://monday.com", "Go to monday!"),
		"phone":     PhoneValue("+12025550169", "US"),
		"dropdown":  DropdownLabelsValue("Marketing", "Engineering"),
		"dropdown2": {Type: ColumnTypeDropdown, DropdownLabelIDs: []int{1, 2}},
		"timeline":  TimelineValue("2026-03-01", "2026-03-15"),
		"cleared":   ClearedValue(ColumnTypeNumbers),
	}, "columnValues")
	require.NoError(t, err)
	require.JSONEq(t, `{
		"text": "Sample text",
		"long_text": {"text": "Line one\nLine two"},
		"numbers": "42.5",
		"status": {"label": "Done"},
		"status2": {"index": 1},
		"date": {"date": "2026-06-15", "time": "09:00:00"},
		"people": {"personsAndTeams": [{"id": 48202303, "kind": "person"}, {"id": 51166, "kind": "team"}]},
		"checkbox": {"checked": "true"},
		"unchecked": null,
		"email": {"email": "contact@example.com", "text": "Main Contact"},
		"email2": {"email": "jane@example.com", "text": "jane@example.com"},
		"link": {"url": "https://monday.com", "text": "Go to monday!"},
		"phone": {"phone": "+12025550169", "countryShortName": "US"},
		"dropdown": {"labels": ["Marketing", "Engineering"]},
		"dropdown2": {"ids": [1, 2]},
		"timeline": {"from": "2026-03-01", "to": "2026-03-15"},
		"cleared": null
	}`, encoded, "numbers are strings and clearing is null, because an empty numbers string sets 0")
}

func TestColumnValuesRejectAmbiguousOrInvalidValues(t *testing.T) {
	negative, nan := -1, 0.0
	nan = nan / nan
	for name, value := range map[string]ColumnValue{
		"unknown type":            {Type: "mirror", Text: "x"},
		"blank text":              {Type: ColumnTypeText},
		"long text over limit":    LongTextValue(strings.Repeat("x", MaxColumnTextCharacters+1)),
		"number not finite":       {Type: ColumnTypeNumbers, Number: &nan},
		"number missing":          {Type: ColumnTypeNumbers},
		"status label and index":  {Type: ColumnTypeStatus, StatusLabel: "Done", StatusIndex: &negative},
		"status negative index":   {Type: ColumnTypeStatus, StatusIndex: &negative},
		"status two-line label":   StatusLabelValue("Done\nNow"),
		"date not a date":         DateValue("2026-02-30"),
		"date bad time":           DateTimeValue("2026-02-18", "9am"),
		"people empty":            {Type: ColumnTypePeople},
		"people zero ID":          PeopleValue(0),
		"checkbox missing":        {Type: ColumnTypeCheckbox},
		"email display name":      EmailValue("Jane <jane@example.com>"),
		"link not absolute":       LinkValue("monday.com", ""),
		"phone without country":   {Type: ColumnTypePhone, Phone: "+12025550169"},
		"dropdown labels and ids": {Type: ColumnTypeDropdown, DropdownLabels: []string{"a"}, DropdownLabelIDs: []int{1}},
		"timeline reversed":       TimelineValue("2026-03-15", "2026-03-01"),
		"text on a status":        {Type: ColumnTypeStatus, StatusLabel: "Done", Text: "Done"},
		"cleared with a value":    {Type: ColumnTypeText, IsCleared: true, Text: "x"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := encodeColumnValues(map[string]ColumnValue{"column": value}, "columnValues")
			require.Error(t, err)
			require.Contains(t, err.Error(), `columnValues "column"`)
		})
	}
	_, err := encodeColumnValues(map[string]ColumnValue{"bad id!": TextValue("x")}, "columnValues")
	require.Error(t, err)
	tooMany := map[string]ColumnValue{}
	for index := range MaxRequestedColumns + 1 {
		tooMany["text"+strings.Repeat("x", index)] = TextValue("x")
	}
	_, err = encodeColumnValues(tooMany, "columnValues")
	require.Error(t, err)
}
