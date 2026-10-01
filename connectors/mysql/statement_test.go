// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mysql

import (
	"database/sql/driver"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestBindStatementConvertsEveryParameterToATypedDriverValue(t *testing.T) {
	moment := time.Date(2026, 1, 1, 5, 30, 0, 123456789, time.FixedZone("IST", 5*3600+1800))
	statement, err := bindStatement("SELECT ?", []any{
		nil, "text", true, 42, int8(-8), int16(16), int32(32), int64(math.MaxInt64), uint(7), uint8(8), uint16(16), uint32(32), uint64(math.MaxUint64),
		float32(0.1), 0.1, json.Number("12345678901234567890.1"), json.RawMessage(`{"a":1}`), []byte{0, 1, 255}, []byte(nil), moment,
	}, statementKindRead)
	require.NoError(t, err)
	values := make([]driver.Value, len(statement.parameterValues))
	for index, value := range statement.parameterValues {
		require.Equal(t, index+1, value.Ordinal)
		values[index] = value.Value
	}
	require.Equal(t, []driver.Value{
		nil, "text", true, int64(42), int64(-8), int64(16), int64(32), int64(math.MaxInt64), uint64(7), uint64(8), uint64(16), uint64(32), uint64(math.MaxUint64),
		0.1, 0.1, "12345678901234567890.1", `{"a":1}`, []byte{0, 1, 255}, nil, "2026-01-01 00:00:00.123456",
	}, values, "float32 keeps its shortest decimal; time.Time is UTC truncated to microseconds")
}

func TestBindStatementRejectsUnsafeOrUnsupportedParameters(t *testing.T) {
	for name, parameter := range map[string]any{
		"map":            map[string]any{"a": 1},
		"slice":          []any{1, 2},
		"struct":         struct{}{},
		"pointer":        new(string),
		"invalid UTF-8":  "\xff",
		"invalid number": json.Number("1e"),
		"invalid JSON":   json.RawMessage(`{"a":`),
		"nil JSON":       json.RawMessage(nil),
		"NaN":            math.NaN(),
		"infinite float": float32(math.Inf(1)),
		"year 10000":     time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
		"year zero":      time.Date(0, 12, 31, 0, 0, 0, 0, time.UTC),
	} {
		t.Run(name, func(t *testing.T) {
			_, err := bindStatement("SELECT ?", []any{parameter}, statementKindRead)
			require.ErrorContains(t, err, "parameter 1")
		})
	}
	_, err := bindStatement("SELECT 1", make([]any, maximumParameters+1), statementKindRead)
	require.ErrorContains(t, err, "more than 65535 parameters")
	_, err = bindStatement("SELECT ?", []any{strings.Repeat("x", maximumParameterBytes+1)}, statementKindRead)
	require.ErrorContains(t, err, "parameters exceed")
}

func TestBindStatementAcceptsOnlyTheOperationsLeadingKeywords(t *testing.T) {
	for _, statement := range []string{
		"SELECT 1", "select * from t", "WITH recent AS (SELECT 1) SELECT * FROM recent", "TABLE t", "VALUES ROW(1)",
		"SHOW TABLES", "EXPLAIN SELECT 1", "DESCRIBE t", "desc t", "(SELECT 1) UNION (SELECT 2)",
		"/* commit */ SELECT 1", "-- rollback\nSELECT 1", "# drop\nSELECT 1", "/*+ MAX_EXECUTION_TIME(1) */ SELECT 1",
	} {
		_, err := bindStatement(statement, nil, statementKindRead)
		require.NoError(t, err, "%q", statement)
	}
	for _, statement := range []string{
		"", "   \n\t", "INSERT INTO t VALUES (1)", "UPDATE t SET a = 1", "CREATE TABLE t (id int)", "DROP TABLE t",
		"START TRANSACTION", "BEGIN", "COMMIT", "SET autocommit = 1", "LOCK TABLES t READ", "CALL p()", "DO SLEEP(1)",
		"/*!50000 DROP TABLE t */ SELECT 1", "/*M!100100 DROP TABLE t */ SELECT 1", "--1\nSELECT 1", "/* unterminated SELECT 1",
		"SELECT 1\x00", "SELECT '\xff'", strings.Repeat("x", maximumStatementBytes+1),
	} {
		_, err := bindStatement(statement, nil, statementKindRead)
		require.Error(t, err, "%q", statement)
	}
	for _, statement := range []string{
		"INSERT INTO t VALUES (1)", "update t set a = 1", "DELETE FROM t", "REPLACE INTO t VALUES (1)",
		"WITH stale AS (SELECT id FROM t) DELETE FROM t WHERE id IN (SELECT id FROM stale)",
		"  /* note */ INSERT INTO t VALUES (1) ON DUPLICATE KEY UPDATE a = a",
	} {
		_, err := bindStatement(statement, nil, statementKindWrite)
		require.NoError(t, err, "%q", statement)
	}
	for _, statement := range []string{
		"SELECT 1", "ALTER TABLE t ADD c int", "TRUNCATE t", "RENAME TABLE a TO b", "LOAD DATA LOCAL INFILE 'x' INTO TABLE t",
		"START TRANSACTION", "XA START 'x'", "SAVEPOINT a", "CALL p()", "SET @a = 1", "LOCK TABLES t WRITE", "GRANT SELECT ON t TO u",
	} {
		_, err := bindStatement(statement, nil, statementKindWrite)
		require.Error(t, err, "%q", statement)
	}
}

func TestLeadingKeywordFollowsMySQLCommentRules(t *testing.T) {
	require.Equal(t, "SELECT", leadingKeyword("  select 1"))
	require.Equal(t, "COMMIT", leadingKeyword("-- a\n# b\ncommit"))
	require.Equal(t, "UPDATE", leadingKeyword("/* a */update"))
	require.Equal(t, "SELECT", leadingKeyword("((select 1))"))
	require.Equal(t, "", leadingKeyword("--1\nSELECT 1"), "-- without a following space is not a comment in MySQL")
	require.Equal(t, "", leadingKeyword("/*!40101 SET NAMES utf8 */"), "MySQL runs executable comments")
	require.Equal(t, "", leadingKeyword("-- only a comment"))
	require.Equal(t, "", leadingKeyword("/* unterminated"))
	require.Equal(t, "", leadingKeyword("/* a */ /*! DROP TABLE t */ SELECT 1"), "an executable comment after a regular one")
	require.Equal(t, "", leadingKeyword("(/*!50000 DROP TABLE t */ SELECT 1)"), "an executable comment inside a parenthesis")
	require.Equal(t, "SELECT", leadingKeyword("--\tcomment\nSELECT 1"), "-- before a tab is a comment")
	require.Equal(t, "", leadingKeyword("# only a comment"), "# without a newline comments out the rest")
	require.Equal(t, "", leadingKeyword("/*/ SELECT 1"), "/*/ opens a comment that never closes")
}

func TestDescribeKeywordsListsTheAcceptedKeywords(t *testing.T) {
	require.Equal(t, "DELETE, INSERT, REPLACE, UPDATE, or WITH", describeKeywords(writeLeadingKeywords))
}
