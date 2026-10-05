// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDecodeColumnValueMapsJSONv2TextWithoutLosingPrecision(t *testing.T) {
	for name, testCase := range map[string]struct {
		column Column
		text   string
		want   any
	}{
		"small integer":         {Column{Type: "fixed", Precision: 9}, "-42", int64(-42)},
		"wide integer":          {Column{Type: "fixed", Precision: 38}, "9007199254740993", "9007199254740993"},
		"decimal":               {Column{Type: "fixed", Precision: 12, Scale: 2}, "1200.00", "1200.00"},
		"float":                 {Column{Type: "real"}, "1.5e+10", 1.5e10},
		"nan":                   {Column{Type: "real"}, "NaN", "NaN"},
		"infinity":              {Column{Type: "real"}, "inf", "Infinity"},
		"decfloat":              {Column{Type: "decfloat"}, "1.23e-40", "1.23e-40"},
		"text":                  {Column{Type: "TEXT"}, "plain", "plain"},
		"boolean":               {Column{Type: "boolean"}, "true", true},
		"binary":                {Column{Type: "binary"}, "0001ff", "AAH/"},
		"date":                  {Column{Type: "date"}, "18262", "2020-01-01"},
		"negative date":         {Column{Type: "date"}, "-1", "1969-12-31"},
		"time":                  {Column{Type: "time"}, "82919.500000000", "23:01:59.5"},
		"timestamp_ntz":         {Column{Type: "timestamp_ntz"}, "1611871777.123456789", "2021-01-28T22:09:37.123456789"},
		"timestamp before 1970": {Column{Type: "timestamp_ntz"}, "-1.500000000", "1969-12-31T23:59:58.5"},
		"timestamp_ltz":         {Column{Type: "timestamp_ltz"}, "1616173619.000000000", "2021-03-19T17:06:59Z"},
		"timestamp_tz":          {Column{Type: "timestamp_tz"}, "1616173619.000000000 1500", "2021-03-19T18:06:59+01:00"},
		"variant":               {Column{Type: "variant"}, "{\n  \"amount\": 12345678901234567890\n}", json.RawMessage("{\n  \"amount\": 12345678901234567890\n}")},
		"geography":             {Column{Type: "geography"}, "POINT(1 2)", "POINT(1 2)"},
	} {
		t.Run(name, func(t *testing.T) {
			column := testCase.column
			column.Type = lowerType(column.Type)
			value, err := decodeColumnValue(column, &testCase.text)
			require.NoError(t, err)
			require.Equal(t, testCase.want, value)
		})
	}
	value, err := decodeColumnValue(Column{Type: "fixed"}, nil)
	require.NoError(t, err)
	require.Nil(t, value, "SQL NULL is JSON null")
}

func TestDecodeColumnValueRejectsTextThatDoesNotMatchItsType(t *testing.T) {
	for name, testCase := range map[string]struct {
		column Column
		text   string
	}{
		"fixed":        {Column{Type: "fixed", Precision: 38}, "12abc"},
		"real":         {Column{Type: "real"}, "fast"},
		"boolean":      {Column{Type: "boolean"}, "maybe"},
		"binary":       {Column{Type: "binary"}, "zz"},
		"date":         {Column{Type: "date"}, "2020-01-01"},
		"timestamp":    {Column{Type: "timestamp_ltz"}, "2021-03-19"},
		"zone offset":  {Column{Type: "timestamp_tz"}, "1616173619.000000000"},
		"zone range":   {Column{Type: "timestamp_tz"}, "1616173619.000000000 9999"},
		"semi-struct":  {Column{Type: "object"}, "{not json"},
		"fraction len": {Column{Type: "time"}, "1.0000000001"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeColumnValue(testCase.column, &testCase.text)
			require.Error(t, err)
		})
	}
}

func TestEncodeBindingChoosesTheDocumentedBindingTypes(t *testing.T) {
	for name, testCase := range map[string]struct {
		value     any
		wantType  string
		wantValue *string
	}{
		"null":        {nil, "TEXT", nil},
		"text":        {"abc", "TEXT", pointerTo("abc")},
		"boolean":     {true, "BOOLEAN", pointerTo("true")},
		"integer":     {int64(-7), "FIXED", pointerTo("-7")},
		"unsigned":    {uint64(18446744073709551615), "FIXED", pointerTo("18446744073709551615")},
		"float":       {0.25, "REAL", pointerTo("0.25")},
		"json int":    {json.Number("9007199254740993"), "FIXED", pointerTo("9007199254740993")},
		"json dec":    {json.Number("250.00"), "TEXT", pointerTo("250.00")},
		"json object": {json.RawMessage(`{"a":1}`), "TEXT", pointerTo(`{"a":1}`)},
		"bytes":       {[]byte{0, 1, 255}, "BINARY", pointerTo("0001ff")},
		"utc time":    {time.Date(2021, 3, 19, 17, 6, 59, 0, time.UTC), "TIMESTAMP_TZ", pointerTo("1616173619000000000 1440")},
	} {
		t.Run(name, func(t *testing.T) {
			binding, err := encodeBinding(testCase.value)
			require.NoError(t, err)
			require.Equal(t, statementBinding{Type: testCase.wantType, Value: testCase.wantValue}, binding)
		})
	}
	_, err := encodeBinding(time.Date(1500, 1, 1, 0, 0, 0, 0, time.UTC))
	require.Error(t, err, "a time outside epoch nanoseconds is rejected")
	_, err = encodeBinding("nul\x00")
	require.Error(t, err)
}

func TestCountQuestionMarkPlaceholdersSkipsLiteralsIdentifiersAndComments(t *testing.T) {
	count, err := countQuestionMarkPlaceholders("SELECT ?, 'it''s ?', 'esc\\'?', \"col?\", $$?$$ -- ?\n, ? // ?\n /* ? */ , ?")
	require.NoError(t, err)
	require.Equal(t, 3, count)
	_, err = countQuestionMarkPlaceholders(`SELECT "open`)
	require.Error(t, err)
	_, err = countQuestionMarkPlaceholders("SELECT /* open")
	require.Error(t, err)
}

func lowerType(columnType string) string {
	columns, err := describeColumns([]rowTypeColumn{{Name: "C", Type: columnType}})
	if err != nil {
		return columnType
	}
	return columns[0].Type
}

func pointerTo(value string) *string { return &value }
