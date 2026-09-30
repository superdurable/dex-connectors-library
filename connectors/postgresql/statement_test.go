// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postgresql

import (
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

func TestBindStatementEncodesEveryParameterAsPostgreSQLText(t *testing.T) {
	moment := time.Date(2026, 1, 1, 5, 30, 0, 123456000, time.FixedZone("IST", 5*3600+1800))
	statement, err := bindStatement("SELECT $1, $2", []any{
		nil, "text", true, 42, int8(-8), int16(16), int32(32), int64(math.MaxInt64), uint(7), uint16(16), uint32(32), uint64(math.MaxUint64),
		float32(1.5), 0.1, math.NaN(), math.Inf(1), math.Inf(-1), json.Number("12345678901234567890.1"),
		json.RawMessage(`{"a":1}`), []byte{0, 1, 255}, moment,
	})
	require.NoError(t, err)
	values := make([]any, len(statement.parameterValues))
	for index, value := range statement.parameterValues {
		if value == nil {
			values[index] = nil
			continue
		}
		values[index] = string(value)
	}
	require.Equal(t, []any{
		nil, "text", "true", "42", "-8", "16", "32", "9223372036854775807", "7", "16", "32", "18446744073709551615",
		"1.5", "0.1", "NaN", "Infinity", "-Infinity", "12345678901234567890.1",
		`{"a":1}`, `\x0001ff`, "2026-01-01T05:30:00.123456+05:30",
	}, values)
}

func TestBindStatementRejectsUnsafeOrUnsupportedParameters(t *testing.T) {
	for name, parameter := range map[string]any{
		"map":            map[string]any{"a": 1},
		"slice":          []any{1, 2},
		"struct":         struct{}{},
		"pointer":        new(string),
		"NUL":            "a\x00b",
		"invalid UTF-8":  "\xff",
		"invalid number": json.Number("1e"),
		"invalid JSON":   json.RawMessage(`{"a":`),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := bindStatement("SELECT $1", []any{parameter})
			require.ErrorContains(t, err, "parameter $1")
			require.NotContains(t, err.Error(), "a\x00b")
		})
	}
	_, err := bindStatement("SELECT 1", make([]any, maximumParameters+1))
	require.ErrorContains(t, err, "more than 65535 parameters")
	_, err = bindStatement("SELECT $1", []any{strings.Repeat("x", maximumParameterBytes+1)})
	require.ErrorContains(t, err, "parameters exceed")
}

func TestBindStatementRejectsStatementsTheConnectorOwnsOrCannotRun(t *testing.T) {
	for _, statement := range []string{
		"", "   \n\t", "BEGIN", "begin transaction", "start transaction read write", "COMMIT", "end", "ROLLBACK",
		"abort", "SAVEPOINT a", "RELEASE SAVEPOINT a", "PREPARE TRANSACTION 'x'", "copy t from stdin",
		"-- leading comment\nCOMMIT", "/* outer /* nested */ still comment */ rollback", "\n  /* c */ -- d\n  Begin;",
		"SELECT 1\x00", "SELECT '\xff'", strings.Repeat("x", maximumStatementBytes+1),
	} {
		_, err := bindStatement(statement, nil)
		require.Error(t, err, "%q", statement)
	}
	for _, statement := range []string{
		"SELECT 1", "with recent as (select 1) select * from recent", "/* commit */ SELECT 1", "-- rollback\nSELECT 1",
		"UPDATE t SET committed = true", "INSERT INTO beginnings VALUES ($1)", "(SELECT 1)", "VALUES (1)",
	} {
		_, err := bindStatement(statement, nil)
		require.NoError(t, err, "%q", statement)
	}
}

func TestLeadingKeywordSkipsWhitespaceAndComments(t *testing.T) {
	require.Equal(t, "SELECT", leadingKeyword("  select 1"))
	require.Equal(t, "COMMIT", leadingKeyword("-- a\n-- b\ncommit"))
	require.Equal(t, "UPDATE", leadingKeyword("/* a /* b */ c */update"))
	require.Equal(t, "", leadingKeyword("-- only a comment"))
	require.Equal(t, "", leadingKeyword("/* unterminated"))
	require.Equal(t, "", leadingKeyword("(select 1)"))
}

func TestValidateStatementDescriptionChecksPlaceholdersAndColumns(t *testing.T) {
	description := &pgconn.StatementDescription{ParamOIDs: []uint32{0, 0}, Fields: []pgconn.FieldDescription{{Name: "a"}, {Name: "b"}}}
	require.NoError(t, validateStatementDescription(description, 2))
	require.EqualError(t, validateStatementDescription(description, 1), "statement uses 2 placeholders but 1 parameters were bound")
	duplicate := &pgconn.StatementDescription{Fields: []pgconn.FieldDescription{{Name: "id"}, {Name: "id"}}}
	require.ErrorContains(t, validateStatementDescription(duplicate, 0), `column "id" more than once`)
}

func TestCommandNameDropsCounts(t *testing.T) {
	require.Equal(t, "INSERT", commandName("INSERT 0 1"))
	require.Equal(t, "UPDATE", commandName("UPDATE 3"))
	require.Equal(t, "CREATE TABLE", commandName("CREATE TABLE"))
	require.Equal(t, "", commandName(""))
}
