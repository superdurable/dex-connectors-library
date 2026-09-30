// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package postgresql

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5/pgconn"
)

const (
	// maximumStatementBytes bounds application-authored SQL text.
	maximumStatementBytes = 1 << 20
	// maximumParameters is the extended-protocol limit on bound parameters.
	maximumParameters = math.MaxUint16
	// maximumParameterBytes bounds the combined text of every bound parameter.
	maximumParameterBytes = 16 << 20
	// maximumConfiguredRows bounds the maxRows connection setting.
	maximumConfiguredRows = 100000
	// maximumConfiguredResponseBytes bounds the maxResponseBytes connection setting.
	maximumConfiguredResponseBytes = 16 << 20
)

var jsonNumberPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

// connectorOwnedLeadingKeywords are statements that would end, split, or escape the connector's own transaction.
var connectorOwnedLeadingKeywords = map[string]string{
	"ABORT":     "transaction control is owned by the connector",
	"BEGIN":     "transaction control is owned by the connector",
	"COMMIT":    "transaction control is owned by the connector",
	"END":       "transaction control is owned by the connector",
	"PREPARE":   "prepared statements and two-phase commit are owned by the connector",
	"RELEASE":   "transaction control is owned by the connector",
	"ROLLBACK":  "transaction control is owned by the connector",
	"SAVEPOINT": "transaction control is owned by the connector",
	"START":     "transaction control is owned by the connector",
	"COPY":      "COPY streams are not supported; use SELECT, INSERT, UPDATE, DELETE, or MERGE",
}

// boundStatement is validated SQL text with every parameter already encoded as PostgreSQL text.
type boundStatement struct {
	sql             string
	parameterValues [][]byte
}

// bindStatement validates the statement and encodes parameters in placeholder order.
func bindStatement(statement string, parameters []any) (boundStatement, error) {
	if err := validateStatementText(statement); err != nil {
		return boundStatement{}, err
	}
	if len(parameters) > maximumParameters {
		return boundStatement{}, fmt.Errorf("statement cannot bind more than %d parameters", maximumParameters)
	}
	values := make([][]byte, len(parameters))
	totalBytes := 0
	for index, parameter := range parameters {
		value, err := encodeParameter(parameter)
		if err != nil {
			return boundStatement{}, fmt.Errorf("parameter $%d: %w", index+1, err)
		}
		totalBytes += len(value)
		if totalBytes > maximumParameterBytes {
			return boundStatement{}, fmt.Errorf("parameters exceed %d bytes combined", maximumParameterBytes)
		}
		values[index] = value
	}
	return boundStatement{sql: statement, parameterValues: values}, nil
}

func validateStatementText(statement string) error {
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
	if reason, isOwned := connectorOwnedLeadingKeywords[keyword]; isOwned {
		return fmt.Errorf("statement cannot start with %s: %s", keyword, reason)
	}
	return nil
}

// leadingKeyword returns the first SQL word in upper case after whitespace and comments.
func leadingKeyword(statement string) string {
	remaining := statement
	for {
		remaining = strings.TrimLeftFunc(remaining, unicode.IsSpace)
		switch {
		case strings.HasPrefix(remaining, "--"):
			newline := strings.IndexByte(remaining, '\n')
			if newline < 0 {
				return ""
			}
			remaining = remaining[newline+1:]
		case strings.HasPrefix(remaining, "/*"):
			remaining = skipBlockComment(remaining)
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

// skipBlockComment removes one leading block comment, honoring PostgreSQL's nesting.
func skipBlockComment(text string) string {
	depth := 0
	for index := 0; index < len(text)-1; index++ {
		switch {
		case text[index] == '/' && text[index+1] == '*':
			depth++
			index++
		case text[index] == '*' && text[index+1] == '/':
			depth--
			index++
			if depth == 0 {
				return text[index+1:]
			}
		}
	}
	return ""
}

// encodeParameter converts one Go value to the PostgreSQL text PostgreSQL parses for the placeholder's type.
func encodeParameter(parameter any) ([]byte, error) {
	switch value := parameter.(type) {
	case nil:
		return nil, nil
	case string:
		return encodeTextParameter(value)
	case bool:
		return []byte(strconv.FormatBool(value)), nil
	case int:
		return []byte(strconv.FormatInt(int64(value), 10)), nil
	case int8:
		return []byte(strconv.FormatInt(int64(value), 10)), nil
	case int16:
		return []byte(strconv.FormatInt(int64(value), 10)), nil
	case int32:
		return []byte(strconv.FormatInt(int64(value), 10)), nil
	case int64:
		return []byte(strconv.FormatInt(value, 10)), nil
	case uint:
		return []byte(strconv.FormatUint(uint64(value), 10)), nil
	case uint8:
		return []byte(strconv.FormatUint(uint64(value), 10)), nil
	case uint16:
		return []byte(strconv.FormatUint(uint64(value), 10)), nil
	case uint32:
		return []byte(strconv.FormatUint(uint64(value), 10)), nil
	case uint64:
		return []byte(strconv.FormatUint(value, 10)), nil
	case float32:
		return []byte(formatFloatParameter(float64(value), 32)), nil
	case float64:
		return []byte(formatFloatParameter(value, 64)), nil
	case json.Number:
		if !jsonNumberPattern.MatchString(string(value)) {
			return nil, fmt.Errorf("json.Number is not a JSON number")
		}
		return []byte(value), nil
	case json.RawMessage:
		if !json.Valid(value) {
			return nil, fmt.Errorf("json.RawMessage is not valid JSON")
		}
		return encodeTextParameter(string(value))
	case []byte:
		encoded := make([]byte, 2+hex.EncodedLen(len(value)))
		copy(encoded, `\x`)
		hex.Encode(encoded[2:], value)
		return encoded, nil
	case time.Time:
		return []byte(value.Format(time.RFC3339Nano)), nil
	default:
		return nil, fmt.Errorf("unsupported Go type %T; pass nil, a string, bool, integer, float, json.Number, json.RawMessage, []byte, or time.Time", parameter)
	}
}

func encodeTextParameter(value string) ([]byte, error) {
	if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
		return nil, fmt.Errorf("text must be valid UTF-8 without NUL characters")
	}
	return []byte(value), nil
}

func formatFloatParameter(value float64, bitSize int) string {
	switch {
	case math.IsNaN(value):
		return "NaN"
	case math.IsInf(value, 1):
		return "Infinity"
	case math.IsInf(value, -1):
		return "-Infinity"
	default:
		return strconv.FormatFloat(value, 'g', -1, bitSize)
	}
}

// validateStatementDescription checks the server's parsed statement before any row is read or written.
func validateStatementDescription(description *pgconn.StatementDescription, boundParameterCount int) error {
	if len(description.ParamOIDs) != boundParameterCount {
		return fmt.Errorf("statement uses %d placeholders but %d parameters were bound", len(description.ParamOIDs), boundParameterCount)
	}
	seen := make(map[string]bool, len(description.Fields))
	for _, field := range description.Fields {
		if seen[field.Name] {
			return fmt.Errorf("statement returns column %q more than once; give each result column a unique alias", field.Name)
		}
		seen[field.Name] = true
	}
	return nil
}
