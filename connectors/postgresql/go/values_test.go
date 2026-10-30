// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postgresql

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecodeColumnValueMapsPostgreSQLTextToExactJSONValues(t *testing.T) {
	for name, testCase := range map[string]struct {
		typeOID uint32
		text    string
		want    any
	}{
		"true bool":                 {boolTypeOID, "t", true},
		"false bool":                {boolTypeOID, "f", false},
		"int2":                      {int2TypeOID, "-32768", int64(-32768)},
		"int4":                      {int4TypeOID, "2147483647", int64(2147483647)},
		"int8 beyond 2^53":          {int8TypeOID, "9007199254740993", "9007199254740993"},
		"negative int8":             {int8TypeOID, "-9223372036854775808", "-9223372036854775808"},
		"float4":                    {float4TypeOID, "1.5", 1.5},
		"float8":                    {float8TypeOID, "0.1", 0.1},
		"float8 exponent":           {float8TypeOID, "1e+308", 1e308},
		"float8 NaN":                {float8TypeOID, "NaN", "NaN"},
		"float8 negative infinity":  {float8TypeOID, "-Infinity", "-Infinity"},
		"numeric keeps every digit": {numericTypeOID, "12345678901234567890.123456789", "12345678901234567890.123456789"},
		"numeric trailing zeros":    {numericTypeOID, "250.00", "250.00"},
		"numeric NaN":               {numericTypeOID, "NaN", "NaN"},
		"numeric infinity":          {numericTypeOID, "-Infinity", "-Infinity"},
		"bytea base64":              {byteaTypeOID, `\x0001ff`, "AAH/"},
		"empty bytea":               {byteaTypeOID, `\x`, ""},
		"timestamptz RFC 3339":      {timestamptzTypeOID, "2026-01-01 00:00:00.123456+00", "2026-01-01T00:00:00.123456Z"},
		"timestamptz whole second":  {timestamptzTypeOID, "2026-01-01 00:00:00+00", "2026-01-01T00:00:00Z"},
		"timestamptz infinity":      {timestamptzTypeOID, "infinity", "infinity"},
		"timestamptz BC":            {timestamptzTypeOID, "0044-03-15 00:00:00+00 BC", "0044-03-15 00:00:00+00 BC"},
		"timestamptz year 10000":    {timestamptzTypeOID, "10000-01-01 00:00:00+00", "10000-01-01 00:00:00+00"},
		"timestamp without zone":    {timestampTypeOID, "2026-01-01 12:34:56.5", "2026-01-01T12:34:56.5"},
		"timestamp -infinity":       {timestampTypeOID, "-infinity", "-infinity"},
		"text":                      {25, "héllo 世界", "héllo 世界"},
		"uuid":                      {2950, "5f0c7a1e-8f6b-4d8a-9f3c-2d1e0b9a8c7d", "5f0c7a1e-8f6b-4d8a-9f3c-2d1e0b9a8c7d"},
		"date":                      {1082, "2026-02-03", "2026-02-03"},
		"interval":                  {1186, "P1DT2H", "P1DT2H"},
		"array":                     {1007, "{1,2,3}", "{1,2,3}"},
		"enum or extension type":    {98765, "gold", "gold"},
	} {
		t.Run(name, func(t *testing.T) {
			value, err := decodeColumnValue(testCase.typeOID, []byte(testCase.text))
			require.NoError(t, err)
			require.Equal(t, testCase.want, value)
		})
	}
}

func TestDecodeColumnValueKeepsJSONVerbatimAndNullAsNil(t *testing.T) {
	value, err := decodeColumnValue(jsonbTypeOID, []byte(`{"amount": 1.10, "id": 9007199254740993}`))
	require.NoError(t, err)
	encoded, err := json.Marshal(map[string]any{"document": value})
	require.NoError(t, err)
	require.Equal(t, `{"document":{"amount":1.10,"id":9007199254740993}}`, string(encoded), "JSON numbers are not rounded")

	value, err = decodeColumnValue(numericTypeOID, nil)
	require.NoError(t, err)
	require.Nil(t, value)
}

func TestDecodeColumnValueRejectsTextThatDoesNotMatchItsType(t *testing.T) {
	for name, testCase := range map[string]struct {
		typeOID uint32
		text    string
	}{
		"bool word":          {boolTypeOID, "true"},
		"int4 overflow":      {int4TypeOID, "2147483648"},
		"int8 decimal":       {int8TypeOID, "1.5"},
		"float word":         {float8TypeOID, "large"},
		"numeric word":       {numericTypeOID, "twelve"},
		"bytea escape":       {byteaTypeOID, `abc`},
		"bytea odd hex":      {byteaTypeOID, `\x0`},
		"json":               {jsonTypeOID, `{"a":`},
		"timestamptz offset": {timestamptzTypeOID, "January 1 2026"},
		"invalid UTF-8":      {25, "\xff"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeColumnValue(testCase.typeOID, []byte(testCase.text))
			require.Error(t, err)
		})
	}
}

func TestDecodeRowNamesTheColumnButNeverTheValue(t *testing.T) {
	columns := []Column{{Name: "status", TypeName: "bool", TypeOID: boolTypeOID}}
	_, err := decodeRow(columns, [][]byte{[]byte("secret-value")})
	require.EqualError(t, err, `column "status" with type OID 16 returned text the connector cannot decode`)

	_, err = decodeRow(columns, [][]byte{[]byte("t"), []byte("t")})
	require.Error(t, err, "a row wider than its description is rejected")
}
