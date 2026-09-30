// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postgresql

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgproto3"
)

// Built-in PostgreSQL type OIDs are fixed by the server catalog and never change between releases.
const (
	boolTypeOID        uint32 = 16
	byteaTypeOID       uint32 = 17
	int8TypeOID        uint32 = 20
	int2TypeOID        uint32 = 21
	int4TypeOID        uint32 = 23
	jsonTypeOID        uint32 = 114
	float4TypeOID      uint32 = 700
	float8TypeOID      uint32 = 701
	timestampTypeOID   uint32 = 1114
	timestamptzTypeOID uint32 = 1184
	numericTypeOID     uint32 = 1700
	jsonbTypeOID       uint32 = 3802
)

var builtInTypeNames = map[uint32]string{
	16: "bool", 17: "bytea", 18: "char", 19: "name", 20: "int8", 21: "int2", 23: "int4", 25: "text", 26: "oid",
	114: "json", 142: "xml", 650: "cidr", 700: "float4", 701: "float8", 790: "money", 829: "macaddr", 869: "inet",
	1042: "bpchar", 1043: "varchar", 1082: "date", 1083: "time", 1114: "timestamp", 1184: "timestamptz",
	1186: "interval", 1266: "timetz", 1560: "bit", 1562: "varbit", 1700: "numeric", 2950: "uuid", 3802: "jsonb",
	4072: "jsonpath",
}

var (
	integerTextPattern  = regexp.MustCompile(`^-?[0-9]+$`)
	numericTextPattern  = regexp.MustCompile(`^(?:-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?|NaN|-?Infinity)$`)
	extendedYearPattern = regexp.MustCompile(`^[0-9]{5,}-`)
)

// Column describes one result column.
type Column struct {
	// Name is the column name PostgreSQL reports, such as the alias given with AS.
	Name string `json:"name"`
	// TypeName is the built-in PostgreSQL type name, such as numeric; it is empty for arrays,
	// enums, domains over other types, and extension types.
	TypeName string `json:"typeName,omitempty"`
	// TypeOID is PostgreSQL's type OID. Look up a type without a TypeName in pg_type.
	TypeOID uint32 `json:"typeOid"`
}

// rowRead is one statement's decoded rows and its command tag.
type rowRead struct {
	columns     []Column
	rows        []map[string]any
	commandTag  pgconn.CommandTag
	isTruncated bool
}

// undecodableValueError names the column whose text did not match its type; it never carries the value.
type undecodableValueError struct {
	column  string
	typeOID uint32
}

// Error names the column and type OID without the undecodable text.
func (err *undecodableValueError) Error() string {
	return fmt.Sprintf("column %q with type OID %d returned text the connector cannot decode", err.column, err.typeOID)
}

// readBoundedRows stops mid-result at a bound, so the caller must then close the connection.
func readBoundedRows(ctx context.Context, connection *pgconn.PgConn, description *pgconn.StatementDescription,
	parameterValues [][]byte, maxRows int, maxResponseBytes int,
) (rowRead, error) {
	read := rowRead{columns: describeColumns(description.Fields), rows: make([]map[string]any, 0)}
	resultReader := connection.ExecPrepared(ctx, description.Name, parameterValues, nil, nil)
	encodedBytes := len("[]")
	for resultReader.NextRow() {
		if len(read.rows) == maxRows {
			read.isTruncated = true
			return read, nil
		}
		row, err := decodeRow(read.columns, resultReader.Values())
		if err != nil {
			return read, err
		}
		encodedRow, err := json.Marshal(row)
		if err != nil {
			return read, &undecodableValueError{column: "*", typeOID: 0}
		}
		encodedBytes += len(encodedRow) + len(",")
		if encodedBytes > maxResponseBytes {
			read.isTruncated = true
			return read, nil
		}
		read.rows = append(read.rows, row)
	}
	commandTag, err := resultReader.Close()
	var oversizedMessage *pgproto3.ExceededMaxBodyLenErr
	if errors.As(err, &oversizedMessage) {
		read.isTruncated = true
		return read, nil
	}
	read.commandTag = commandTag
	return read, err
}

func describeColumns(fields []pgconn.FieldDescription) []Column {
	columns := make([]Column, len(fields))
	for index, field := range fields {
		columns[index] = Column{Name: field.Name, TypeName: builtInTypeNames[field.DataTypeOID], TypeOID: field.DataTypeOID}
	}
	return columns
}

func decodeRow(columns []Column, values [][]byte) (map[string]any, error) {
	if len(values) != len(columns) {
		return nil, &undecodableValueError{column: "*", typeOID: 0}
	}
	row := make(map[string]any, len(columns))
	for index, column := range columns {
		value, err := decodeColumnValue(column.TypeOID, values[index])
		if err != nil {
			return nil, &undecodableValueError{column: column.Name, typeOID: column.TypeOID}
		}
		row[column.Name] = value
	}
	return row, nil
}

// decodeColumnValue maps PostgreSQL's text output to a JSON value without losing precision.
func decodeColumnValue(typeOID uint32, text []byte) (any, error) {
	if text == nil {
		return nil, nil
	}
	if !utf8.Valid(text) {
		return nil, errors.New("value is not UTF-8")
	}
	value := string(text)
	switch typeOID {
	case boolTypeOID:
		switch value {
		case "t":
			return true, nil
		case "f":
			return false, nil
		}
		return nil, errors.New("invalid bool")
	case int2TypeOID, int4TypeOID:
		return strconv.ParseInt(value, 10, 32)
	case int8TypeOID:
		if !integerTextPattern.MatchString(value) {
			return nil, errors.New("invalid int8")
		}
		return value, nil
	case float4TypeOID, float8TypeOID:
		return decodeFloat(value)
	case numericTypeOID:
		if !numericTextPattern.MatchString(value) {
			return nil, errors.New("invalid numeric")
		}
		return value, nil
	case byteaTypeOID:
		return decodeBytea(value)
	case jsonTypeOID, jsonbTypeOID:
		if !json.Valid(text) {
			return nil, errors.New("invalid json")
		}
		return json.RawMessage(value), nil
	case timestamptzTypeOID:
		return decodeTimestamp(value, "2006-01-02 15:04:05.999999999-07", time.RFC3339Nano)
	case timestampTypeOID:
		return decodeTimestamp(value, "2006-01-02 15:04:05.999999999", "2006-01-02T15:04:05.999999999")
	default:
		return value, nil
	}
}

func decodeFloat(value string) (any, error) {
	switch value {
	case "NaN", "Infinity", "-Infinity":
		return value, nil
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
		return nil, errors.New("invalid float")
	}
	return number, nil
}

func decodeBytea(value string) (any, error) {
	encoded, hasHexPrefix := strings.CutPrefix(value, `\x`)
	if !hasHexPrefix {
		return nil, errors.New("bytea is not in hex output format")
	}
	decoded, err := hex.DecodeString(encoded)
	if err != nil {
		return nil, errors.New("invalid bytea hex")
	}
	return base64.StdEncoding.EncodeToString(decoded), nil
}

// decodeTimestamp returns ISO 8601 text, or PostgreSQL's text for values ISO 8601 cannot express.
func decodeTimestamp(value string, inputLayout string, outputLayout string) (any, error) {
	if value == "infinity" || value == "-infinity" || strings.HasSuffix(value, " BC") || extendedYearPattern.MatchString(value) {
		return value, nil
	}
	parsed, err := time.Parse(inputLayout, value)
	if err != nil {
		return nil, errors.New("invalid timestamp")
	}
	if outputLayout == time.RFC3339Nano {
		parsed = parsed.UTC()
	}
	return parsed.Format(outputLayout), nil
}
