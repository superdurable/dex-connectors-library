// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver

import (
	"database/sql/driver"
	"math"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestDecodeColumnValueMapsDriverValuesToExactJSON(t *testing.T) {
	offset := time.FixedZone("", -5*3600)
	for name, testCase := range map[string]struct {
		typeName string
		value    driver.Value
		expected any
	}{
		"null":                   {"INT", nil, nil},
		"int":                    {"INT", int64(-2147483648), int64(-2147483648)},
		"bigint beyond 2^53":     {"BIGINT", int64(9007199254740993), "9007199254740993"},
		"bit":                    {"BIT", true, true},
		"real widened by driver": {"REAL", float64(float32(0.1)), 0.1},
		"real from fixed type":   {"REAL", float32(1.5), 1.5},
		"float":                  {"FLOAT", 0.30000000000000004, 0.30000000000000004},
		"decimal":                {"DECIMAL", []byte("-0.0100"), "-0.0100"},
		"money":                  {"MONEY", []byte("12.3400"), "12.3400"},
		"nvarchar":               {"NVARCHAR", "naïve", "naïve"},
		"xml":                    {"XML", "<a/>", "<a/>"},
		"varbinary":              {"VARBINARY", []byte{0xff}, "/w=="},
		"uniqueidentifier":       {"UNIQUEIDENTIFIER", []byte{0xff, 0x19, 0x96, 0x6f, 0x86, 0x8b, 0x11, 0xd0, 0xb4, 0x2d, 0x00, 0xc0, 0x4f, 0xc9, 0x64, 0xff}, "6f9619ff-8b86-d011-b42d-00c04fc964ff"},
		"date":                   {"DATE", time.Date(2026, 2, 3, 0, 0, 0, 0, time.UTC), "2026-02-03"},
		"time":                   {"TIME", time.Date(1, 1, 1, 23, 59, 59, 999999900, time.UTC), "23:59:59.9999999"},
		"datetime2":              {"DATETIME2", time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC), "2026-01-01T12:00:00"},
		"datetimeoffset":         {"DATETIMEOFFSET", time.Date(2026, 1, 1, 7, 0, 0, 500000000, offset), "2026-01-01T07:00:00.5-05:00"},
		"datetimeoffset UTC":     {"DATETIMEOFFSET", time.Date(2026, 1, 1, 7, 0, 0, 0, time.FixedZone("", 0)), "2026-01-01T07:00:00Z"},
		"CLR type bytes":         {"HIERARCHYID", []byte{0x58}, "WA=="},
	} {
		t.Run(name, func(t *testing.T) {
			decoded, err := decodeColumnValue(testCase.typeName, testCase.value)
			require.NoError(t, err)
			require.Equal(t, testCase.expected, decoded)
		})
	}
}

func TestDecodeColumnValueRejectsValuesThatDoNotMatchTheirType(t *testing.T) {
	for name, testCase := range map[string]struct {
		typeName string
		value    driver.Value
	}{
		"text for an integer":     {"INT", "1"},
		"NaN float":               {"FLOAT", math.NaN()},
		"double for a real":       {"REAL", 0.1},
		"malformed decimal":       {"DECIMAL", []byte("1e3")},
		"invalid UTF-8":           {"NVARCHAR", string([]byte{0xff})},
		"short uniqueidentifier":  {"UNIQUEIDENTIFIER", []byte{1, 2}},
		"text for a date":         {"DATE", "2026-01-01"},
		"sql_variant":             {"SQL_VARIANT", int64(1)},
		"unknown type with a map": {"MYTYPE", map[string]any{}},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := decodeColumnValue(testCase.typeName, testCase.value)
			require.Error(t, err)
		})
	}
}

func TestDecodeRowNamesTheColumnButNotTheValue(t *testing.T) {
	_, err := decodeRow([]Column{{Name: "amount", TypeName: "DECIMAL"}}, []driver.Value{[]byte("customer-card-4242")})
	require.EqualError(t, err, `column "amount" of type DECIMAL returned a value the connector cannot decode`)
}
