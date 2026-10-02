// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver

import (
	"database/sql/driver"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"regexp"
	"strconv"
	"time"
	"unicode/utf8"
)

// Driver type names, from the driver's ColumnTypeDatabaseTypeName, grouped by the JSON value they map to.
var (
	numberTypeNames  = map[string]bool{"TINYINT": true, "SMALLINT": true, "INT": true}
	decimalTypeNames = map[string]bool{"DECIMAL": true, "MONEY": true, "SMALLMONEY": true}
	textTypeNames    = map[string]bool{
		"CHAR": true, "VARCHAR": true, "TEXT": true, "NCHAR": true, "NVARCHAR": true, "NTEXT": true, "XML": true,
	}
	binaryTypeNames   = map[string]bool{"BINARY": true, "VARBINARY": true, "IMAGE": true}
	dateTimeTypeNames = map[string]bool{"DATETIME2": true, "DATETIME": true, "SMALLDATETIME": true}
)

var decimalTextPattern = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)

const (
	dateLayout           = "2006-01-02"
	timeLayout           = "15:04:05.9999999"
	dateTimeLayout       = "2006-01-02T15:04:05.9999999"
	dateTimeOffsetLayout = "2006-01-02T15:04:05.9999999Z07:00"
)

// Column describes one result column.
type Column struct {
	// Name is the column name the server reports, such as the alias given with AS.
	Name string `json:"name"`
	// TypeName is the server's column type, such as DECIMAL, BIGINT, NVARCHAR, VARBINARY,
	// UNIQUEIDENTIFIER, or DATETIMEOFFSET. NUMERIC is reported as DECIMAL, and a CLR type such as
	// GEOGRAPHY or HIERARCHYID by its own name.
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

// resultShapeError rejects a result that a row map cannot represent or that the operation did not expect.
type resultShapeError struct{ message string }

// Error describes the result shape problem.
func (err *resultShapeError) Error() string { return err.message }

// readBoundedRows stops mid-result at a bound; close then drains the rest before closing the connection.
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
	// Replies after the rows, such as COMMIT, get their own budget.
	defer meter.armReadBudget(controlReadBudgetBytes)
	values := make([]driver.Value, len(columns))
	encodedBytes := len("[]")
	for {
		err := callDriver(func() error { return rows.Next(values) })
		if errors.Is(err, io.EOF) {
			return read, requireSingleResultSet(rows)
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

// requireSingleResultSet rejects a second result set, which only a second statement can produce.
func requireSingleResultSet(rows driver.Rows) error {
	nextResultSet, hasNextResultSet := rows.(driver.RowsNextResultSet)
	if hasNextResultSet && nextResultSet.HasNextResultSet() {
		return &resultShapeError{message: "the statement returned more than one result set; run one statement per call"}
	}
	return nil
}

func describeColumns(rows driver.Rows) ([]Column, error) {
	var columns []Column
	err := callDriver(func() error {
		names := rows.Columns()
		typeNames, hasTypeNames := rows.(driver.RowsColumnTypeDatabaseTypeName)
		columns = make([]Column, len(names))
		seen := make(map[string]bool, len(names))
		for index, name := range names {
			if name == "" {
				return &resultShapeError{message: fmt.Sprintf("result column %d has no name; give every result column an alias with AS", index+1)}
			}
			if seen[name] {
				return &resultShapeError{message: fmt.Sprintf("statement returns column %q more than once; give each result column a unique alias", name)}
			}
			seen[name] = true
			columns[index] = Column{Name: name}
			if hasTypeNames {
				columns[index].TypeName = typeNames.ColumnTypeDatabaseTypeName(index)
			}
		}
		return nil
	})
	return columns, err
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

// decodeColumnValue maps one driver value to an exact JSON value.
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
	case typeName == "BIT":
		flag, isBool := value.(bool)
		if !isBool {
			return nil, errors.New("invalid bit")
		}
		return flag, nil
	case typeName == "REAL":
		return decodeReal(value)
	case typeName == "FLOAT":
		number, isFloat := value.(float64)
		if !isFloat || math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, errors.New("invalid float")
		}
		return number, nil
	case decimalTypeNames[typeName]:
		text, isText := value.([]byte)
		if !isText || !decimalTextPattern.Match(text) {
			return nil, errors.New("invalid decimal")
		}
		return string(text), nil
	case textTypeNames[typeName]:
		text, isText := value.(string)
		if !isText || !utf8.ValidString(text) {
			return nil, errors.New("invalid text")
		}
		return text, nil
	case binaryTypeNames[typeName]:
		bytes, isBytes := value.([]byte)
		if !isBytes {
			return nil, errors.New("invalid binary")
		}
		return base64.StdEncoding.EncodeToString(bytes), nil
	case typeName == "UNIQUEIDENTIFIER":
		return formatUniqueIdentifier(value)
	case typeName == "DATE":
		return formatTime(value, dateLayout)
	case typeName == "TIME":
		return formatTime(value, timeLayout)
	case dateTimeTypeNames[typeName]:
		return formatTime(value, dateTimeLayout)
	case typeName == "DATETIMEOFFSET":
		return formatTime(value, dateTimeOffsetLayout)
	case typeName == "SQL_VARIANT":
		return nil, errors.New("sql_variant is not supported")
	}
	// A CLR type such as GEOGRAPHY, GEOMETRY, or HIERARCHYID arrives as its serialized bytes.
	if bytes, isBytes := value.([]byte); isBytes {
		return base64.StdEncoding.EncodeToString(bytes), nil
	}
	return nil, errors.New("unsupported type")
}

// decodeReal undoes the driver's widening of a 4-byte float, so 0.1 stays 0.1.
func decodeReal(value driver.Value) (any, error) {
	var number float64
	switch typed := value.(type) {
	case float32:
		number = float64(typed)
	case float64:
		number = typed
	default:
		return nil, errors.New("invalid real")
	}
	if math.IsNaN(number) || math.IsInf(number, 0) || float64(float32(number)) != number {
		return nil, errors.New("invalid real")
	}
	return strconv.ParseFloat(strconv.FormatFloat(number, 'g', -1, 32), 64)
}

// formatUniqueIdentifier reads SQL Server's mixed-endian wire bytes into the canonical lowercase form.
func formatUniqueIdentifier(value driver.Value) (any, error) {
	wire, isBytes := value.([]byte)
	if !isBytes || len(wire) != 16 {
		return nil, errors.New("invalid uniqueidentifier")
	}
	canonical := []byte{
		wire[3], wire[2], wire[1], wire[0], wire[5], wire[4], wire[7], wire[6],
		wire[8], wire[9], wire[10], wire[11], wire[12], wire[13], wire[14], wire[15],
	}
	text := hex.EncodeToString(canonical)
	return text[0:8] + "-" + text[8:12] + "-" + text[12:16] + "-" + text[16:20] + "-" + text[20:32], nil
}

func formatTime(value driver.Value, layout string) (any, error) {
	instant, isTime := value.(time.Time)
	if !isTime {
		return nil, errors.New("invalid temporal value")
	}
	if layout != dateTimeOffsetLayout {
		// The driver decodes types without an offset in UTC, so the wall-clock fields are the stored ones.
		instant = instant.UTC()
	}
	return instant.Format(layout), nil
}
