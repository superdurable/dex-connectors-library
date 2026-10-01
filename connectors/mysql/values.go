// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mysql

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"
)

// Driver type names, from the driver's ColumnTypeDatabaseTypeName, grouped by the JSON value they map to.
var (
	numberTypeNames = map[string]bool{
		"TINYINT": true, "UNSIGNED TINYINT": true, "SMALLINT": true, "UNSIGNED SMALLINT": true,
		"MEDIUMINT": true, "UNSIGNED MEDIUMINT": true, "INT": true, "UNSIGNED INT": true, "YEAR": true,
	}
	binaryTypeNames = map[string]bool{
		"BINARY": true, "VARBINARY": true, "TINYBLOB": true, "BLOB": true, "MEDIUMBLOB": true, "LONGBLOB": true,
		"GEOMETRY": true, "VECTOR": true,
	}
)

var (
	decimalTextPattern  = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)
	dateTimeTextPattern = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}(?: [0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,6})?)?$`)
	timeTextPattern     = regexp.MustCompile(`^-?[0-9]{2,3}:[0-9]{2}:[0-9]{2}(?:\.[0-9]{1,6})?$`)
)

// Column describes one result column.
type Column struct {
	// Name is the column name the server reports, such as the alias given with AS.
	Name string `json:"name"`
	// TypeName is the server's column type, such as DECIMAL, UNSIGNED BIGINT, VARCHAR, BLOB, JSON, or TIMESTAMP.
	// MariaDB reports a JSON column as LONGTEXT and an expression's type may differ from a table column's.
	TypeName string `json:"typeName"`
}

type rowRead struct {
	columns     []Column
	rows        []map[string]any
	isTruncated bool
}

// undecodableValueError names the column whose value did not match its type; it never carries the value.
type undecodableValueError struct {
	column   string
	typeName string
}

// Error names the column and type without the undecodable value.
func (err *undecodableValueError) Error() string {
	return fmt.Sprintf("column %q of type %s returned a value the connector cannot decode", err.column, err.typeName)
}

// duplicateColumnError rejects a result that a row map cannot represent.
type duplicateColumnError struct{ column string }

// Error names the repeated column.
func (err *duplicateColumnError) Error() string {
	return fmt.Sprintf("statement returns column %q more than once; give each result column a unique alias", err.column)
}

// readBoundedRows stops mid-result at a bound, so the caller must then close the session instead of the rows.
func readBoundedRows(rows driver.Rows, meter *connectionMeter, maxRows int, maxResponseBytes int) (rowRead, error) {
	columns, err := describeColumns(rows)
	if err != nil {
		return rowRead{columns: []Column{}, rows: []map[string]any{}}, err
	}
	read := rowRead{columns: columns, rows: make([]map[string]any, 0)}
	if len(columns) == 0 {
		return read, nil
	}
	meter.armReadBudget(int64(maxResponseBytes) + protocolOverheadBytes)
	// Replies after the rows, such as ROW_COUNT() and COMMIT, get their own budget.
	defer meter.armReadBudget(controlReadBudgetBytes)
	values := make([]driver.Value, len(columns))
	encodedBytes := len("[]")
	for {
		err := rows.Next(values)
		if errors.Is(err, io.EOF) {
			return read, nil
		}
		if err != nil {
			if meter.hasExceededReadBudget() {
				read.isTruncated = true
				return read, nil
			}
			return read, err
		}
		if len(read.rows) == maxRows {
			read.isTruncated = true
			return read, nil
		}
		row, err := decodeRow(columns, values)
		if err != nil {
			return read, err
		}
		encodedRow, err := json.Marshal(row)
		if err != nil {
			return read, &undecodableValueError{column: "*", typeName: "row"}
		}
		encodedBytes += len(encodedRow) + len(",")
		if encodedBytes > maxResponseBytes {
			read.isTruncated = true
			return read, nil
		}
		read.rows = append(read.rows, row)
	}
}

func describeColumns(rows driver.Rows) ([]Column, error) {
	names := rows.Columns()
	typeNames, hasTypeNames := rows.(driver.RowsColumnTypeDatabaseTypeName)
	columns := make([]Column, len(names))
	seen := make(map[string]bool, len(names))
	for index, name := range names {
		if seen[name] {
			return nil, &duplicateColumnError{column: name}
		}
		seen[name] = true
		columns[index] = Column{Name: name}
		if hasTypeNames {
			columns[index].TypeName = typeNames.ColumnTypeDatabaseTypeName(index)
		}
	}
	return columns, nil
}

func decodeRow(columns []Column, values []driver.Value) (map[string]any, error) {
	row := make(map[string]any, len(columns))
	for index, column := range columns {
		value, err := decodeColumnValue(column.TypeName, values[index])
		if err != nil {
			return nil, &undecodableValueError{column: column.Name, typeName: column.TypeName}
		}
		row[column.Name] = value
	}
	return row, nil
}

// decodeColumnValue maps one driver value to an exact JSON value, copying bytes out of the driver's read buffer.
func decodeColumnValue(typeName string, value driver.Value) (any, error) {
	if value == nil {
		return nil, nil
	}
	switch {
	case numberTypeNames[typeName]:
		integer, isInteger := value.(int64)
		if !isInteger {
			return nil, errors.New("invalid integer")
		}
		return integer, nil
	case typeName == "BIGINT":
		integer, isInteger := value.(int64)
		if !isInteger {
			return nil, errors.New("invalid bigint")
		}
		return strconv.FormatInt(integer, 10), nil
	case typeName == "UNSIGNED BIGINT":
		return formatUnsignedInteger(value)
	case typeName == "FLOAT":
		number, isFloat := value.(float32)
		if !isFloat || math.IsNaN(float64(number)) || math.IsInf(float64(number), 0) {
			return nil, errors.New("invalid float")
		}
		return strconv.ParseFloat(strconv.FormatFloat(float64(number), 'g', -1, 32), 64)
	case typeName == "DOUBLE":
		number, isFloat := value.(float64)
		if !isFloat || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, errors.New("invalid double")
		}
		return number, nil
	case typeName == "NULL":
		return nil, nil
	}
	text, isText := value.([]byte)
	if !isText {
		return nil, errors.New("expected length-encoded bytes")
	}
	switch {
	case typeName == "DECIMAL":
		if !decimalTextPattern.Match(text) {
			return nil, errors.New("invalid decimal")
		}
		return string(text), nil
	case binaryTypeNames[typeName]:
		return base64.StdEncoding.EncodeToString(text), nil
	case typeName == "BIT":
		return new(big.Int).SetBytes(text).String(), nil
	case typeName == "JSON":
		if !json.Valid(text) {
			return nil, errors.New("invalid json")
		}
		return json.RawMessage(append([]byte(nil), text...)), nil
	case typeName == "DATETIME":
		return decodeDateTime(string(text), "2006-01-02T15:04:05.999999999")
	case typeName == "TIMESTAMP":
		return decodeDateTime(string(text), time.RFC3339Nano)
	case typeName == "DATE":
		if !dateTimeTextPattern.Match(text) || len(text) != len("2006-01-02") {
			return nil, errors.New("invalid date")
		}
		return string(text), nil
	case typeName == "TIME":
		if !timeTextPattern.Match(text) {
			return nil, errors.New("invalid time")
		}
		return string(text), nil
	}
	if !utf8.Valid(text) {
		return nil, errors.New("text is not UTF-8")
	}
	return string(text), nil
}

// decodeDateTime returns ISO 8601 text, or MySQL's text for a zero or partial date ISO 8601 cannot express.
func decodeDateTime(value string, outputLayout string) (any, error) {
	if !dateTimeTextPattern.MatchString(value) {
		return nil, errors.New("invalid datetime")
	}
	inputLayout := "2006-01-02 15:04:05.999999"
	if len(value) == len("2006-01-02") {
		inputLayout = "2006-01-02"
	}
	parsed, err := time.Parse(inputLayout, value)
	if err != nil {
		return value, nil
	}
	return parsed.UTC().Format(outputLayout), nil
}
