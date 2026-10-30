// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake

import (
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
)

const (
	// maximumExactIntegerPrecision is the most digits a JSON number holds exactly in a float64 reader.
	maximumExactIntegerPrecision = 15
	secondsPerDay                = 86400
)

var (
	decimalTextPattern    = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?$`)
	decfloatTextPattern   = regexp.MustCompile(`^-?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)
	epochSecondsPattern   = regexp.MustCompile(`^(-?)([0-9]{1,12})(?:\.([0-9]{1,9}))?$`)
	epochDaysTextPattern  = regexp.MustCompile(`^-?[0-9]{1,9}$`)
	timestampOffsetFormat = regexp.MustCompile(`^[0-9]{1,4}$`)
)

// Column describes one result column from Snowflake's resultSetMetaData.rowType.
type Column struct {
	// Name is the column name Snowflake reports, upper case unless the statement quoted an alias.
	Name string `json:"name"`
	// Type is the lower-case SQL API type, such as fixed, real, decfloat, text, boolean, binary,
	// date, time, timestamp_ntz, timestamp_ltz, timestamp_tz, variant, object, or array.
	Type string `json:"type"`
	// Precision is the number of digits of a fixed column, such as 38 for NUMBER.
	Precision int64 `json:"precision,omitempty"`
	// Scale is the number of fractional digits of a fixed column, or of a time or timestamp column.
	Scale int64 `json:"scale,omitempty"`
	// Length is the maximum length of a text or binary column.
	Length int64 `json:"length,omitempty"`
	// Nullable reports whether the column can hold SQL NULL.
	Nullable bool `json:"nullable"`
}

// undecodableValueError names the column whose text did not match its type; it never carries the value.
type undecodableValueError struct {
	column     string
	columnType string
}

// Error names the column and its type without the undecodable text.
func (err *undecodableValueError) Error() string {
	return fmt.Sprintf("column %q of type %s returned text the connector cannot decode", err.column, err.columnType)
}

// describeColumns converts rowType and rejects names a row map cannot hold.
func describeColumns(rowType []rowTypeColumn) ([]Column, error) {
	columns := make([]Column, len(rowType))
	for index, column := range rowType {
		if column.Name == "" || !utf8.ValidString(column.Name) || column.Type == "" {
			return nil, errResponseShapeInvalid
		}
		columns[index] = Column{Name: column.Name, Type: strings.ToLower(column.Type)}
		if column.Precision != nil {
			columns[index].Precision = *column.Precision
		}
		if column.Scale != nil {
			columns[index].Scale = *column.Scale
		}
		if column.Length != nil {
			columns[index].Length = *column.Length
		}
		if column.Nullable != nil {
			columns[index].Nullable = *column.Nullable
		}
	}
	return columns, nil
}

// validateUniqueColumnNames is a statement defect: two equal names cannot share one row map.
func validateUniqueColumnNames(columns []Column) error {
	seen := make(map[string]bool, len(columns))
	for _, column := range columns {
		if seen[column.Name] {
			return fmt.Errorf("the statement returns column %q more than once; give each result column a unique alias", column.Name)
		}
		seen[column.Name] = true
	}
	return nil
}

// convertRows maps raw rows to JSON values and stops at maxResponseBytes of encoded rows.
func convertRows(columns []Column, rawRows [][]*string, maxResponseBytes int) ([]map[string]any, bool, error) {
	rows := make([]map[string]any, 0, len(rawRows))
	encodedBytes := len("[]")
	for _, rawRow := range rawRows {
		if len(rawRow) != len(columns) {
			return nil, false, errResponseShapeInvalid
		}
		row := make(map[string]any, len(columns))
		for index, column := range columns {
			value, err := decodeColumnValue(column, rawRow[index])
			if err != nil {
				return nil, false, &undecodableValueError{column: column.Name, columnType: column.Type}
			}
			row[column.Name] = value
		}
		encodedRow, err := json.Marshal(row)
		if err != nil {
			return nil, false, &undecodableValueError{column: "*", columnType: "row"}
		}
		encodedBytes += len(encodedRow) + len(",")
		if encodedBytes > maxResponseBytes {
			return rows, true, nil
		}
		rows = append(rows, row)
	}
	return rows, false, nil
}

// decodeColumnValue maps Snowflake's jsonv2 text to a JSON value without losing precision.
func decodeColumnValue(column Column, text *string) (any, error) {
	if text == nil {
		return nil, nil
	}
	value := *text
	if !utf8.ValidString(value) {
		return nil, errors.New("value is not UTF-8")
	}
	switch column.Type {
	case "fixed":
		return decodeFixed(column, value)
	case "real":
		return decodeReal(value)
	case "decfloat":
		if !decfloatTextPattern.MatchString(value) {
			return nil, errors.New("invalid decfloat")
		}
		return value, nil
	case "boolean":
		return strconv.ParseBool(value)
	case "binary":
		decoded, err := hex.DecodeString(value)
		if err != nil {
			return nil, errors.New("invalid binary hex")
		}
		return base64.StdEncoding.EncodeToString(decoded), nil
	case "date":
		return decodeDate(value)
	case "time":
		return decodeEpochTime(value, "15:04:05.999999999", false)
	case "timestamp_ntz":
		return decodeEpochTime(value, "2006-01-02T15:04:05.999999999", true)
	case "timestamp_ltz":
		return decodeEpochTime(value, time.RFC3339Nano, true)
	case "timestamp_tz":
		return decodeZonedTimestamp(value)
	case "variant", "object", "array", "map":
		if !json.Valid([]byte(value)) {
			return nil, errors.New("invalid semi-structured JSON")
		}
		return json.RawMessage(value), nil
	default:
		return value, nil
	}
}

// decodeFixed returns a number only when the column's precision fits a float64 exactly, otherwise exact decimal text.
func decodeFixed(column Column, value string) (any, error) {
	if !decimalTextPattern.MatchString(value) {
		return nil, errors.New("invalid fixed")
	}
	if column.Scale == 0 && column.Precision > 0 && column.Precision <= maximumExactIntegerPrecision {
		return strconv.ParseInt(value, 10, 64)
	}
	return value, nil
}

func decodeReal(value string) (any, error) {
	switch strings.ToLower(value) {
	case "nan":
		return "NaN", nil
	case "inf", "infinity":
		return "Infinity", nil
	case "-inf", "-infinity":
		return "-Infinity", nil
	}
	number, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsInf(number, 0) || math.IsNaN(number) {
		return nil, errors.New("invalid real")
	}
	return number, nil
}

// decodeDate converts days since the epoch to YYYY-MM-DD, or keeps the count when ISO 8601 cannot express the year.
func decodeDate(value string) (any, error) {
	if !epochDaysTextPattern.MatchString(value) {
		return nil, errors.New("invalid date")
	}
	days, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return nil, err
	}
	date := time.Unix(days*secondsPerDay, 0).UTC()
	if date.Year() < 1 || date.Year() > 9999 {
		return value, nil
	}
	return date.Format(time.DateOnly), nil
}

// decodeEpochTime converts seconds since the epoch with up to nine fractional digits.
func decodeEpochTime(value string, layout string, isTimestamp bool) (any, error) {
	instant, err := parseEpochSeconds(value)
	if err != nil {
		return nil, err
	}
	if isTimestamp && (instant.Year() < 1 || instant.Year() > 9999) {
		return value, nil
	}
	return instant.UTC().Format(layout), nil
}

// decodeZonedTimestamp reads "seconds offset", where the offset is minutes plus 1440.
func decodeZonedTimestamp(value string) (any, error) {
	seconds, offsetText, hasOffset := strings.Cut(value, " ")
	if !hasOffset || !timestampOffsetFormat.MatchString(offsetText) {
		return nil, errors.New("invalid timestamp_tz offset")
	}
	encodedOffset, err := strconv.Atoi(offsetText)
	if err != nil {
		return nil, err
	}
	offsetMinutes := encodedOffset - timestampOffsetBase
	if offsetMinutes < -24*60 || offsetMinutes > 24*60 {
		return nil, errors.New("timestamp_tz offset out of range")
	}
	instant, err := parseEpochSeconds(seconds)
	if err != nil {
		return nil, err
	}
	zoned := instant.In(time.FixedZone("", offsetMinutes*60))
	if zoned.Year() < 1 || zoned.Year() > 9999 {
		return value, nil
	}
	return zoned.Format(time.RFC3339Nano), nil
}

func parseEpochSeconds(value string) (time.Time, error) {
	parts := epochSecondsPattern.FindStringSubmatch(value)
	if parts == nil {
		return time.Time{}, errors.New("invalid epoch seconds")
	}
	seconds, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil {
		return time.Time{}, err
	}
	nanoseconds := int64(0)
	if parts[3] != "" {
		nanoseconds, err = strconv.ParseInt(parts[3]+strings.Repeat("0", 9-len(parts[3])), 10, 64)
		if err != nil {
			return time.Time{}, err
		}
	}
	if parts[1] == "-" {
		seconds, nanoseconds = -seconds, -nanoseconds
	}
	return time.Unix(seconds, nanoseconds), nil
}
