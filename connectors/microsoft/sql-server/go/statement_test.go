// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver

import (
	"database/sql/driver"
	"encoding/json"
	"math"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestStatementTextAcceptsOneStatementOfTheOperationsKind(t *testing.T) {
	for statement, kind := range map[string]statementKind{
		"SELECT id FROM dbo.t WHERE a = @p1":                                                                                                                statementKindRead,
		"  -- leading comment\n/* block /* nested */ */ (SELECT id FROM dbo.t WHERE a = @p1)":                                                               statementKindRead,
		"WITH ids AS (SELECT id FROM dbo.t WHERE a = @p1) SELECT id FROM ids;":                                                                              statementKindRead,
		"SELECT [commit], \"begin\", N'EXEC; DROP' AS [weird;name] FROM dbo.t WHERE a = @p1":                                                                statementKindRead,
		"SELECT id FROM dbo.t WHERE a = @p1 OPTION (USE HINT ('DISABLE_OPTIMIZER_ROWGOAL'))":                                                                statementKindRead,
		"SELECT TOP (10) id, @@ROWCOUNT AS counted FROM dbo.t WHERE a = @p1 ORDER BY id OFFSET 0 ROWS FETCH NEXT 10 ROWS ONLY":                              statementKindRead,
		"INSERT INTO dbo.t (a) VALUES (@p1)":                                                                                                                statementKindWrite,
		"UPDATE dbo.t SET a = @p1 WHERE id = 1":                                                                                                             statementKindWrite,
		"DELETE FROM dbo.t OUTPUT DELETED.id WHERE a = @p1":                                                                                                 statementKindWrite,
		"MERGE dbo.t AS target USING (SELECT @p1 AS a) AS source ON target.a = source.a WHEN NOT MATCHED THEN INSERT (a) VALUES (source.a) OUTPUT $action;": statementKindWrite,
		"WITH old AS (SELECT id FROM dbo.t WHERE a = @p1) DELETE FROM old":                                                                                  statementKindWrite,
		"INSERT INTO dbo.t (a) SELECT @p1 WHERE NOT EXISTS (SELECT 1 FROM dbo.t WITH (UPDLOCK, HOLDLOCK) WHERE a = @p1)":                                    statementKindWrite,
	} {
		t.Run(statement, func(t *testing.T) {
			require.NoError(t, validateStatementText(statement, kind, 1))
		})
	}
}

func TestStatementTextRejectsWhatCouldEscapeTheConnectorsTransaction(t *testing.T) {
	for name, testCase := range map[string]struct {
		statement string
		kind      statementKind
		message   string
	}{
		"write in query":               {"INSERT INTO dbo.t VALUES (1)", statementKindRead, "start with SELECT, or WITH"},
		"read in execute":              {"SELECT 1 AS one", statementKindWrite, "start with DELETE"},
		"DDL in execute":               {"CREATE TABLE dbo.t (id int)", statementKindWrite, "start with"},
		"second statement":             {"SELECT 1 AS one; SELECT 2 AS two", statementKindRead, "more than one statement"},
		"commit without semicolon":     {"UPDATE dbo.t SET a = 1 COMMIT", statementKindWrite, "COMMIT, which belongs to transaction control"},
		"rollback":                     {"SELECT 1 AS one\nROLLBACK TRAN", statementKindRead, "transaction control"},
		"dynamic SQL":                  {"INSERT INTO dbo.t EXEC dbo.load", statementKindWrite, "dynamic SQL"},
		"database switch":              {"SELECT 1 AS one USE master", statementKindRead, "context switching"},
		"execute as":                   {"SELECT 1 AS one EXECUTE AS USER = 'dbo'", statementKindRead, "dynamic SQL"},
		"declaration":                  {"DELETE FROM dbo.t DECLARE @x int", statementKindWrite, "declarations"},
		"control of flow":              {"UPDATE dbo.t SET a = 1 IF 1 = 1 DELETE FROM dbo.t", statementKindWrite, "control of flow"},
		"ad hoc remote access":         {"SELECT * FROM OPENROWSET(BULK 'c:\\x', SINGLE_CLOB) AS file_contents", statementKindRead, "remote or file access"},
		"session option":               {"UPDATE dbo.t SET a = 1 SET ROWCOUNT 0", statementKindWrite, "session options"},
		"statistics update":            {"UPDATE STATISTICS dbo.t", statementKindWrite, "schema or permission"},
		"unterminated comment":         {"SELECT 1 AS one /* open", statementKindRead, "unterminated block comment"},
		"unterminated bracket":         {"SELECT [one FROM dbo.t", statementKindRead, "unterminated bracketed identifier"},
		"unterminated quoted name":     {"SELECT \"one FROM dbo.t", statementKindRead, "unterminated quoted identifier"},
		"procedure call":               {"SELECT*@p1", statementKindRead, "stored procedure call"},
		"NUL character":                {"SELECT 1\x00", statementKindRead, "NUL"},
		"blank":                        {" \n ", statementKindRead, "required"},
		"undeclared variable":          {"SELECT @total AS t", statementKindRead, "only the positional parameters"},
		"upper-case placeholder":       {"SELECT @P1 AS t", statementKindRead, "lowercase @p1"},
		"leading-zero placeholder":     {"SELECT @p01 AS t", statementKindRead, "without leading zeros"},
		"placeholder beyond the count": {"SELECT @p1, @p2 AS t", statementKindRead, "only 1 parameters are bound"},
	} {
		t.Run(name, func(t *testing.T) {
			err := validateStatementText(testCase.statement, testCase.kind, 1)
			require.ErrorContains(t, err, testCase.message)
		})
	}
	require.ErrorContains(t, validateStatementText("SELECT 1 AS one", statementKindRead, 1), "@p1 is bound but the statement never references it")
	require.ErrorContains(t, validateStatementText(strings.Repeat("x", maximumStatementBytes+1), statementKindRead, 0), "exceeds")
}

func TestReservedKeywordsInsideLiteralsAndDelimitedIdentifiersAreText(t *testing.T) {
	tokens, err := tokenizeStatement("SELECT 'it''s; COMMIT', N'EXEC', [a]]b; DROP], \"x\"\"; USE\" /* GRANT */ -- DENY\nFROM dbo.t")
	require.NoError(t, err)
	var words []string
	for _, token := range tokens {
		if token.kind == tokenWord {
			words = append(words, token.text)
		}
	}
	require.Equal(t, []string{"SELECT", "FROM", "dbo", "t"}, words)
}

func TestIsSentAsProcedureCallMirrorsTheDriver(t *testing.T) {
	for statement, isProcedure := range map[string]bool{
		"dbo.DeleteEverything": true, "[dbo].[my proc]": true, "SELECT": true, "SELECT@p1": true,
		"SELECT 1": false, "SELECT\n1": false, "SELECT(1)": false, "COMMIT": false, "dbo.t;": false,
	} {
		require.Equal(t, isProcedure, isSentAsProcedureCall(statement), statement)
	}
}

func TestBindStatementConvertsParametersToTypedDriverValues(t *testing.T) {
	moment := time.Date(2026, 1, 2, 3, 4, 5, 123456789, time.FixedZone("CET", 3600))
	statement, err := bindStatement("SELECT @p1, @p2, @p3, @p4, @p5, @p6, @p7, @p8, @p9, @p10, @p11, @p12 AS v", []any{
		nil, "héllo", true, int32(-7), uint32(7), float32(0.1), 2.5, json.Number("12.50"), json.RawMessage(`{"a":1}`),
		[]byte{1, 2}, moment, uint64(math.MaxInt64),
	}, 12, statementKindRead)
	require.NoError(t, err)
	values := make([]driver.Value, len(statement.parameterValues))
	for index, parameter := range statement.parameterValues {
		require.Equal(t, index+1, parameter.Ordinal)
		require.Empty(t, parameter.Name, "the driver names positional values @p1 through @pN")
		values[index] = parameter.Value
	}
	require.Equal(t, []driver.Value{
		nil, "héllo", true, int64(-7), int64(7), 0.1, 2.5, "12.50", `{"a":1}`, []byte{1, 2}, moment.UTC(), int64(math.MaxInt64),
	}, values)

	for name, parameter := range map[string]any{
		"NaN":                math.NaN(),
		"infinity":           float32(math.Inf(1)),
		"invalid UTF-8":      string([]byte{0xff}),
		"invalid JSON":       json.RawMessage(`{`),
		"unsigned overflow":  uint64(math.MaxInt64) + 1,
		"year 10000":         time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC),
		"unsupported struct": struct{}{},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := bindStatement("SELECT @p1 AS v", []any{parameter}, 1, statementKindRead)
			require.ErrorContains(t, err, "parameter @p1")
		})
	}
	_, err = bindStatement("SELECT 1 AS v", make([]any, maximumParameters+1), maximumParameters+1, statementKindRead)
	require.ErrorContains(t, err, "more than 2098 parameters")
}
