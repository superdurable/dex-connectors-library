// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package excel_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/microsoft/excel"
)

func TestCellValueRoundTripsExcelJSONScalars(t *testing.T) {
	var row []excel.CellValue
	require.NoError(t, json.Unmarshal([]byte(`["travel",1500.25,true,"",null,9007199254740993]`), &row))
	text, isText := row[0].Text()
	require.True(t, isText)
	require.Equal(t, "travel", text)
	number, isNumber := row[1].Number()
	require.True(t, isNumber)
	require.Equal(t, 1500.25, number)
	_, isTextNumber := row[1].Text()
	require.False(t, isTextNumber)
	isTrue, isBoolean := row[2].Boolean()
	require.True(t, isBoolean && isTrue)
	require.True(t, row[3].IsEmpty())
	require.True(t, row[4].IsAbsent())
	require.False(t, row[3].IsAbsent())

	encoded, err := json.Marshal(row[:5])
	require.NoError(t, err)
	require.JSONEq(t, `["travel",1500.25,true,"",null]`, string(encoded), "Results stay plain JSON in Dex Web")
	require.Error(t, json.Unmarshal([]byte(`[[1]]`), &row), "nested arrays are not cell values")
	require.Error(t, json.Unmarshal([]byte(`[{"a":1}]`), &row))
}

func TestCellValueStringMatchesExcelDisplayForBooleans(t *testing.T) {
	require.Equal(t, "TRUE", excel.BooleanCellValue(true).String())
	require.Equal(t, "FALSE", excel.BooleanCellValue(false).String())
	require.Equal(t, "1500.25", excel.NumberCellValue(1500.25).String())
	require.Equal(t, "REQ-1", excel.TextCellValue("REQ-1").String())
	require.Equal(t, "", excel.CellValue{}.String())
	require.True(t, excel.EmptyCellValue().IsEmpty())
	require.False(t, excel.TextCellValue(" ").IsEmpty())
}
