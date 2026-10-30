// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package mysql

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	// maximumStatementBytes bounds application-authored SQL text.
	maximumStatementBytes = 1 << 20
	// maximumParameters is the prepared-statement protocol's limit on bound parameters.
	maximumParameters = math.MaxUint16
	// maximumParameterBytes bounds the combined size of every bound parameter.
	maximumParameterBytes = 16 << 20
	// maximumConfiguredRows bounds the maxRows connection setting.
	maximumConfiguredRows = 100000
	// maximumConfiguredResponseBytes bounds the maxResponseBytes connection setting.
	maximumConfiguredResponseBytes = 16 << 20
	// parameterTimeLayout is MySQL's DATETIME literal at its microsecond precision.
	parameterTimeLayout = "2006-01-02 15:04:05.999999"
)

var jsonNumberPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

// statementKind selects the leading keywords an operation accepts.
type statementKind int

const (
	statementKindRead statementKind = iota
	statementKindWrite
)

// readLeadingKeywords cannot write or commit; MySQL lets DDL escape a READ ONLY transaction by committing implicitly.
var readLeadingKeywords = map[string]bool{
	"SELECT": true, "WITH": true, "TABLE": true, "VALUES": true, "SHOW": true, "EXPLAIN": true, "DESCRIBE": true, "DESC": true,
}

// writeLeadingKeywords run inside the connector's transaction without an implicit commit.
var writeLeadingKeywords = map[string]bool{
	"INSERT": true, "UPDATE": true, "DELETE": true, "REPLACE": true, "WITH": true,
}

// boundStatement is validated SQL text with every parameter already converted to a driver value.
type boundStatement struct {
	sql             string
	parameterValues []driver.NamedValue
}

// bindStatement validates the statement for its operation and converts parameters in placeholder order.
func bindStatement(statement string, parameters []any, kind statementKind) (boundStatement, error) {
	if err := validateStatementText(statement, kind); err != nil {
		return boundStatement{}, err
	}
	if len(parameters) > maximumParameters {
		return boundStatement{}, fmt.Errorf("statement cannot bind more than %d parameters", maximumParameters)
	}
	values := make([]driver.NamedValue, len(parameters))
	totalBytes := 0
	for index, parameter := range parameters {
		value, size, err := encodeParameter(parameter)
		if err != nil {
			return boundStatement{}, fmt.Errorf("parameter %d: %w", index+1, err)
		}
		totalBytes += size
		if totalBytes > maximumParameterBytes {
			return boundStatement{}, fmt.Errorf("parameters exceed %d bytes combined", maximumParameterBytes)
		}
		values[index] = driver.NamedValue{Ordinal: index + 1, Value: value}
	}
	return boundStatement{sql: statement, parameterValues: values}, nil
}

func validateStatementText(statement string, kind statementKind) error {
	if strings.TrimSpace(statement) == "" {
		return fmt.Errorf("statement is required")
	}
	if len(statement) > maximumStatementBytes {
		return fmt.Errorf("statement exceeds %d bytes", maximumStatementBytes)
	}
	if !utf8.ValidString(statement) || strings.ContainsRune(statement, 0) {
		return fmt.Errorf("statement must be valid UTF-8 without NUL characters")
	}
	keyword := leadingKeyword(statement)
	if kind == statementKindRead && !readLeadingKeywords[keyword] {
		return fmt.Errorf("query runs only statements that start with %s; use execute for writes", describeKeywords(readLeadingKeywords))
	}
	if kind == statementKindWrite && !writeLeadingKeywords[keyword] {
		return fmt.Errorf("execute runs only statements that start with %s; statements that commit implicitly, such as CREATE, ALTER, DROP, TRUNCATE, or LOCK TABLES, "+
			"and transaction control, CALL, LOAD DATA, SET, and executable comments are not supported", describeKeywords(writeLeadingKeywords))
	}
	return nil
}

// leadingKeyword returns the first upper-case word after whitespace, comments, and parentheses, or "" at an executable comment.
func leadingKeyword(statement string) string {
	remaining := statement
	for {
		remaining = strings.TrimLeftFunc(remaining, unicode.IsSpace)
		switch {
		case strings.HasPrefix(remaining, "/*!") || strings.HasPrefix(remaining, "/*M!"):
			return ""
		case strings.HasPrefix(remaining, "/*"):
			end := strings.Index(remaining[2:], "*/")
			if end < 0 {
				return ""
			}
			remaining = remaining[2+end+2:]
		case strings.HasPrefix(remaining, "#") || isDashDashComment(remaining):
			newline := strings.IndexByte(remaining, '\n')
			if newline < 0 {
				return ""
			}
			remaining = remaining[newline+1:]
		case strings.HasPrefix(remaining, "("):
			remaining = remaining[1:]
		default:
			end := strings.IndexFunc(remaining, func(character rune) bool {
				return !unicode.IsLetter(character) && character != '_'
			})
			if end < 0 {
				end = len(remaining)
			}
			return strings.ToUpper(remaining[:end])
		}
	}
}

// isDashDashComment applies MySQL's rule that -- starts a comment only before whitespace or a control character.
func isDashDashComment(text string) bool {
	if !strings.HasPrefix(text, "--") {
		return false
	}
	if len(text) == 2 {
		return true
	}
	next := rune(text[2])
	return unicode.IsSpace(next) || unicode.IsControl(next)
}

func describeKeywords(keywords map[string]bool) string {
	names := make([]string, 0, len(keywords))
	for keyword := range keywords {
		names = append(names, keyword)
	}
	sort.Strings(names)
	return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
}

// encodeParameter converts one Go value to the driver value MySQL binds for a ? placeholder, and its size.
func encodeParameter(parameter any) (driver.Value, int, error) {
	switch value := parameter.(type) {
	case nil:
		return nil, 0, nil
	case string:
		if !utf8.ValidString(value) {
			return nil, 0, fmt.Errorf("text must be valid UTF-8")
		}
		return value, len(value), nil
	case bool:
		return value, 1, nil
	case int:
		return int64(value), 8, nil
	case int8:
		return int64(value), 8, nil
	case int16:
		return int64(value), 8, nil
	case int32:
		return int64(value), 8, nil
	case int64:
		return value, 8, nil
	case uint:
		return uint64(value), 8, nil
	case uint8:
		return uint64(value), 8, nil
	case uint16:
		return uint64(value), 8, nil
	case uint32:
		return uint64(value), 8, nil
	case uint64:
		return value, 8, nil
	case float32:
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, 0, fmt.Errorf("MySQL cannot store NaN or Infinity")
		}
		// The shortest float32 text, parsed as float64, avoids binary widening artifacts such as 0.10000000149.
		widened, err := strconv.ParseFloat(strconv.FormatFloat(float64(value), 'g', -1, 32), 64)
		return widened, 8, err
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, 0, fmt.Errorf("MySQL cannot store NaN or Infinity")
		}
		return value, 8, nil
	case json.Number:
		if !jsonNumberPattern.MatchString(string(value)) {
			return nil, 0, fmt.Errorf("json.Number is not a JSON number")
		}
		return string(value), len(value), nil
	case json.RawMessage:
		if value == nil || !json.Valid(value) {
			return nil, 0, fmt.Errorf("json.RawMessage is not valid JSON")
		}
		return string(value), len(value), nil
	case []byte:
		if value == nil {
			return nil, 0, nil
		}
		return value, len(value), nil
	case time.Time:
		utc := value.UTC()
		if utc.Year() < 1 || utc.Year() > 9999 {
			return nil, 0, fmt.Errorf("time.Time must fall in years 0001 through 9999 UTC")
		}
		text := utc.Format(parameterTimeLayout)
		return text, len(text), nil
	default:
		return nil, 0, fmt.Errorf("unsupported Go type %T; pass nil, a string, bool, integer, float, json.Number, json.RawMessage, []byte, or time.Time", parameter)
	}
}

// validatePreparedStatement checks the server's parsed placeholder count before anything executes.
func validatePreparedStatement(statement driver.Stmt, boundParameterCount int) error {
	if placeholderCount := statement.NumInput(); placeholderCount != boundParameterCount {
		return fmt.Errorf("statement uses %d placeholders but %d parameters were bound", placeholderCount, boundParameterCount)
	}
	return nil
}
