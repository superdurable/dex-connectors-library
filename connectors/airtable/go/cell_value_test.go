// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package airtable_test

import (
	"encoding/json"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/airtable"
)

func TestCellValueConstructorsEncodeAirtablesCellFormat(t *testing.T) {
	for name, testCase := range map[string]struct {
		value airtable.CellValue
		json  string
	}{
		"text":                {airtable.TextCellValue(`say "hi"`), `"say \"hi\""`},
		"number":              {airtable.NumberCellValue(-12.5), `-12.5`},
		"checked box":         {airtable.CheckboxCellValue(true), `true`},
		"linked records":      {airtable.LinkedRecordsCellValue("recPolicyStandard"), `["recPolicyStandard"]`},
		"no linked records":   {airtable.LinkedRecordsCellValue(), `[]`},
		"multiple selects":    {airtable.MultipleSelectsCellValue("refund", "eu"), `["refund","eu"]`},
		"no multiple selects": {airtable.MultipleSelectsCellValue(), `[]`},
		"null":                {airtable.NullCellValue(), `null`},
	} {
		t.Run(name, func(t *testing.T) {
			encoded, err := json.Marshal(map[string]airtable.CellValue{"field": testCase.value})
			require.NoError(t, err)
			require.JSONEq(t, `{"field":`+testCase.json+`}`, string(encoded))
		})
	}
	_, err := json.Marshal(airtable.NumberCellValue(math.NaN()))
	require.Error(t, err, "a non-finite number is never encoded as a cell value")
}

func TestCellValueReadersAcceptOnlyTheirJSONType(t *testing.T) {
	var fields map[string]airtable.CellValue
	require.NoError(t, json.Unmarshal([]byte(`{"text":"Ada","number":3,"checked":true,"links":["recPolicyStandard"],
		"mixed":["a",1],"object":{"label":"Open","url":null},"null":null}`), &fields))
	text, isText := fields["text"].Text()
	require.True(t, isText)
	require.Equal(t, "Ada", text)
	_, isText = fields["number"].Text()
	require.False(t, isText)
	number, isNumber := fields["number"].Number()
	require.True(t, isNumber)
	require.Equal(t, 3.0, number)
	_, isNumber = fields["text"].Number()
	require.False(t, isNumber)
	require.True(t, fields["checked"].Checkbox())
	require.False(t, fields["text"].Checkbox())
	links, isList := fields["links"].StringList()
	require.True(t, isList)
	require.Equal(t, []string{"recPolicyStandard"}, links)
	_, isList = fields["mixed"].StringList()
	require.False(t, isList)
	require.True(t, fields["null"].IsNull())
	require.True(t, fields["missing"].IsNull(), "a missing field reads as null")
	require.False(t, fields["missing"].Checkbox())
	require.JSONEq(t, `{"label":"Open","url":null}`, string(fields["object"]), "other field types keep their raw JSON")
}

func TestRecordsRoundTripThroughJSONWithTypedCells(t *testing.T) {
	record := airtable.Record{ID: "recPolicyStandard", Fields: map[string]airtable.CellValue{
		"Limit": airtable.NumberCellValue(250), "Links": airtable.LinkedRecordsCellValue("recOwner000000001"),
	}}
	encoded, err := json.Marshal(record)
	require.NoError(t, err)
	var decoded airtable.Record
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, record, decoded, "Dex persists Results as JSON, so typed cells survive Step boundaries")
}
