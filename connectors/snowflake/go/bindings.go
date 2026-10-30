// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake

import (
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	// maximumStatementBytes is Snowflake's 1 MB limit on SQL text.
	maximumStatementBytes = 1 << 20
	// maximumParameters bounds the bindings one statement may carry.
	maximumParameters = 10000
	// maximumParameterBytes bounds the combined text of every binding.
	maximumParameterBytes = 16 << 20
	// timestampOffsetBase is the 1440 Snowflake adds to TIMESTAMP_TZ offsets in minutes, so they stay positive.
	timestampOffsetBase = 1440
)

var (
	integerTextPattern    = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)$`)
	jsonNumberTextPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)
)

// statementBinding is one SQL API bind variable; Snowflake requires every value as a string or null.
type statementBinding struct {
	Type  string  `json:"type"`
	Value *string `json:"value"`
}

// bindStatement validates the statement and encodes each parameter as the binding for its ? placeholder.
func bindStatement(statement string, parameters []any) (map[string]statementBinding, error) {
	if strings.TrimSpace(statement) == "" {
		return nil, fmt.Errorf("statement is required")
	}
	if len(statement) > maximumStatementBytes {
		return nil, fmt.Errorf("statement exceeds %d bytes", maximumStatementBytes)
	}
	if !utf8.ValidString(statement) || strings.ContainsRune(statement, 0) {
		return nil, fmt.Errorf("statement must be valid UTF-8 without NUL characters")
	}
	if len(parameters) > maximumParameters {
		return nil, fmt.Errorf("statement cannot bind more than %d parameters", maximumParameters)
	}
	placeholderCount, err := countQuestionMarkPlaceholders(statement)
	if err != nil {
		return nil, err
	}
	if placeholderCount != len(parameters) {
		return nil, fmt.Errorf("statement uses %d ? placeholders but %d parameters were bound", placeholderCount, len(parameters))
	}
	if len(parameters) == 0 {
		return nil, nil
	}
	bindings := make(map[string]statementBinding, len(parameters))
	totalBytes := 0
	for index, parameter := range parameters {
		binding, err := encodeBinding(parameter)
		if err != nil {
			return nil, fmt.Errorf("parameter %d: %w", index+1, err)
		}
		if binding.Value != nil {
			totalBytes += len(*binding.Value)
		}
		if totalBytes > maximumParameterBytes {
			return nil, fmt.Errorf("parameters exceed %d bytes combined", maximumParameterBytes)
		}
		bindings[strconv.Itoa(index+1)] = binding
	}
	return bindings, nil
}

// countQuestionMarkPlaceholders counts ? outside string literals, quoted identifiers, and comments.
func countQuestionMarkPlaceholders(statement string) (int, error) {
	count := 0
	for index := 0; index < len(statement); index++ {
		remaining := statement[index:]
		switch {
		case remaining[0] == '?':
			count++
		case remaining[0] == '\'':
			end, err := skipQuotedText(statement, index, '\'', true)
			if err != nil {
				return 0, err
			}
			index = end
		case remaining[0] == '"':
			end, err := skipQuotedText(statement, index, '"', false)
			if err != nil {
				return 0, err
			}
			index = end
		case strings.HasPrefix(remaining, "$$"):
			end := strings.Index(statement[index+2:], "$$")
			if end < 0 {
				return 0, fmt.Errorf("statement has an unterminated $$ string")
			}
			index += 2 + end + 1
		case strings.HasPrefix(remaining, "--") || strings.HasPrefix(remaining, "//"):
			end := strings.IndexByte(remaining, '\n')
			if end < 0 {
				return count, nil
			}
			index += end
		case strings.HasPrefix(remaining, "/*"):
			end := strings.Index(remaining[2:], "*/")
			if end < 0 {
				return 0, fmt.Errorf("statement has an unterminated /* comment")
			}
			index += 2 + end + 1
		}
	}
	return count, nil
}

// skipQuotedText returns the index of the closing quote; a doubled quote, or a backslash in a string literal, escapes.
func skipQuotedText(statement string, start int, quote byte, allowsBackslashEscapes bool) (int, error) {
	for index := start + 1; index < len(statement); index++ {
		switch {
		case allowsBackslashEscapes && statement[index] == '\\':
			index++
		case statement[index] == quote && index+1 < len(statement) && statement[index+1] == quote:
			index++
		case statement[index] == quote:
			return index, nil
		}
	}
	if quote == '"' {
		return 0, fmt.Errorf("statement has an unterminated quoted identifier")
	}
	return 0, fmt.Errorf("statement has an unterminated string literal")
}

// encodeBinding chooses the SQL API binding type for one Go value.
func encodeBinding(parameter any) (statementBinding, error) {
	switch value := parameter.(type) {
	case nil:
		return statementBinding{Type: "TEXT"}, nil
	case string:
		if !utf8.ValidString(value) || strings.ContainsRune(value, 0) {
			return statementBinding{}, fmt.Errorf("text must be valid UTF-8 without NUL characters")
		}
		return textBinding("TEXT", value), nil
	case bool:
		return textBinding("BOOLEAN", strconv.FormatBool(value)), nil
	case int:
		return textBinding("FIXED", strconv.FormatInt(int64(value), 10)), nil
	case int8:
		return textBinding("FIXED", strconv.FormatInt(int64(value), 10)), nil
	case int16:
		return textBinding("FIXED", strconv.FormatInt(int64(value), 10)), nil
	case int32:
		return textBinding("FIXED", strconv.FormatInt(int64(value), 10)), nil
	case int64:
		return textBinding("FIXED", strconv.FormatInt(value, 10)), nil
	case uint:
		return textBinding("FIXED", strconv.FormatUint(uint64(value), 10)), nil
	case uint8:
		return textBinding("FIXED", strconv.FormatUint(uint64(value), 10)), nil
	case uint16:
		return textBinding("FIXED", strconv.FormatUint(uint64(value), 10)), nil
	case uint32:
		return textBinding("FIXED", strconv.FormatUint(uint64(value), 10)), nil
	case uint64:
		return textBinding("FIXED", strconv.FormatUint(value, 10)), nil
	case float32:
		return encodeFloatBinding(float64(value), 32)
	case float64:
		return encodeFloatBinding(value, 64)
	case json.Number:
		return encodeJSONNumberBinding(value)
	case json.RawMessage:
		if !json.Valid(value) {
			return statementBinding{}, fmt.Errorf("json.RawMessage is not valid JSON")
		}
		return textBinding("TEXT", string(value)), nil
	case []byte:
		return textBinding("BINARY", hex.EncodeToString(value)), nil
	case time.Time:
		return encodeTimestampBinding(value)
	default:
		return statementBinding{}, fmt.Errorf(
			"unsupported Go type %T; pass nil, a string, bool, integer, finite float, json.Number, json.RawMessage, []byte, or time.Time", parameter)
	}
}

func encodeFloatBinding(value float64, bitSize int) (statementBinding, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return statementBinding{}, fmt.Errorf("float must be finite")
	}
	return textBinding("REAL", strconv.FormatFloat(value, 'g', -1, bitSize)), nil
}

// encodeJSONNumberBinding keeps integers exact as FIXED and sends other numbers as text Snowflake converts exactly.
func encodeJSONNumberBinding(value json.Number) (statementBinding, error) {
	text := string(value)
	switch {
	case integerTextPattern.MatchString(text):
		return textBinding("FIXED", text), nil
	case jsonNumberTextPattern.MatchString(text):
		return textBinding("TEXT", text), nil
	default:
		return statementBinding{}, fmt.Errorf("json.Number is not a JSON number")
	}
}

// encodeTimestampBinding uses the documented TIMESTAMP_TZ form: epoch nanoseconds, a space, and the offset plus 1440 minutes.
func encodeTimestampBinding(value time.Time) (statementBinding, error) {
	if value.Year() < 1678 || value.Year() > 2261 {
		return statementBinding{}, fmt.Errorf("time.Time must be between the years 1678 and 2261, the range of epoch nanoseconds")
	}
	_, offsetSeconds := value.Zone()
	encoded := strconv.FormatInt(value.UnixNano(), 10) + " " + strconv.Itoa(offsetSeconds/60+timestampOffsetBase)
	return textBinding("TIMESTAMP_TZ", encoded), nil
}

func textBinding(bindingType string, value string) statementBinding {
	return statementBinding{Type: bindingType, Value: &value}
}
