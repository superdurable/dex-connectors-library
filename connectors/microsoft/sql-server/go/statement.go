// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package sqlserver

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
	// maximumParameters leaves room for sp_executesql's @stmt and @params within SQL Server's 2,100 RPC parameters.
	maximumParameters = 2098
	// maximumParameterBytes bounds the combined size of every bound parameter.
	maximumParameterBytes = 16 << 20
	// maximumConfiguredRows bounds the maxRows connection setting.
	maximumConfiguredRows = 100000
	// maximumConfiguredResponseBytes bounds the maxResponseBytes connection setting.
	maximumConfiguredResponseBytes = 16 << 20
	// maximumIdentifierCharacters is SQL Server's limit for a database or user name.
	maximumIdentifierCharacters = 128
)

var jsonNumberPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?$`)

// positionalParameterPattern is the only variable form a statement may reference besides @@ system functions.
var positionalParameterPattern = regexp.MustCompile(`^@p([1-9][0-9]*)$`)

// statementKind selects the leading keywords an operation accepts.
type statementKind int

const (
	statementKindRead statementKind = iota
	statementKindWrite
)

// readLeadingKeywords start a query; a WITH that ends in a write is rolled back with the query's transaction.
var readLeadingKeywords = map[string]bool{"SELECT": true, "WITH": true}

// writeLeadingKeywords start a data modification that runs inside the connector's transaction.
var writeLeadingKeywords = map[string]bool{"INSERT": true, "UPDATE": true, "DELETE": true, "MERGE": true, "WITH": true}

// deniedKeywords start other statements; reserved keywords cannot be undelimited identifiers, so matches are exact.
var deniedKeywords = map[string]string{
	"BEGIN": "transaction control", "COMMIT": "transaction control", "ROLLBACK": "transaction control",
	"SAVE": "transaction control", "TRAN": "transaction control", "TRANSACTION": "transaction control",
	"DISTRIBUTED": "transaction control",
	"EXEC":        "dynamic SQL or procedure calls", "EXECUTE": "dynamic SQL or procedure calls",
	"USE": "context switching", "SETUSER": "context switching", "REVERT": "context switching",
	"IF": "control of flow", "WHILE": "control of flow", "GOTO": "control of flow", "RETURN": "control of flow",
	"BREAK": "control of flow", "CONTINUE": "control of flow", "WAITFOR": "control of flow",
	"DECLARE": "declarations and cursors", "DEALLOCATE": "declarations and cursors",
	"CREATE": "schema or permission changes", "ALTER": "schema or permission changes", "DROP": "schema or permission changes",
	"TRUNCATE": "schema or permission changes", "GRANT": "schema or permission changes", "DENY": "schema or permission changes",
	"REVOKE": "schema or permission changes", "TRIGGER": "schema or permission changes", "STATISTICS": "schema or permission changes",
	"BACKUP": "server administration", "RESTORE": "server administration", "DBCC": "server administration",
	"KILL": "server administration", "SHUTDOWN": "server administration", "RECONFIGURE": "server administration",
	"CHECKPOINT": "server administration", "LOAD": "server administration", "DUMP": "server administration",
	"BULK": "remote or file access", "OPENROWSET": "remote or file access", "OPENDATASOURCE": "remote or file access",
	"OPENQUERY": "remote or file access",
	"ROWCOUNT":  "session options", "TEXTSIZE": "session options", "IDENTITY_INSERT": "session options",
	"PRINT": "messages", "RAISERROR": "messages",
	"READTEXT": "text pointers", "WRITETEXT": "text pointers", "UPDATETEXT": "text pointers",
}

// queryHintKeywords may follow USE inside an OPTION clause, as in OPTION (USE HINT ('...')).
var queryHintKeywords = map[string]bool{"HINT": true, "PLAN": true}

// boundStatement is validated SQL text with every parameter already converted to a driver value.
type boundStatement struct {
	sql             string
	parameterValues []driver.NamedValue
}

// sqlToken is one lexical token outside strings, comments, and delimited identifiers.
type sqlToken struct {
	kind sqlTokenKind
	text string
}

type sqlTokenKind int

const (
	tokenWord sqlTokenKind = iota
	tokenVariable
	tokenSemicolon
	tokenOpenParenthesis
	tokenOther
)

// bindStatement requires @p1 through @p{placeholderCount}, which includes an idempotency key the caller appends.
func bindStatement(statement string, parameters []any, placeholderCount int, kind statementKind) (boundStatement, error) {
	if len(parameters) > maximumParameters || placeholderCount > maximumParameters {
		return boundStatement{}, fmt.Errorf("statement cannot bind more than %d parameters", maximumParameters)
	}
	if err := validateStatementText(statement, kind, placeholderCount); err != nil {
		return boundStatement{}, err
	}
	values := make([]driver.NamedValue, len(parameters))
	totalBytes := 0
	for index, parameter := range parameters {
		value, size, err := encodeParameter(parameter)
		if err != nil {
			return boundStatement{}, fmt.Errorf("parameter @p%d: %w", index+1, err)
		}
		totalBytes += size
		if totalBytes > maximumParameterBytes {
			return boundStatement{}, fmt.Errorf("parameters exceed %d bytes combined", maximumParameterBytes)
		}
		values[index] = driver.NamedValue{Ordinal: index + 1, Value: value}
	}
	return boundStatement{sql: statement, parameterValues: values}, nil
}

func validateStatementText(statement string, kind statementKind, placeholderCount int) error {
	if strings.TrimSpace(statement) == "" {
		return fmt.Errorf("statement is required")
	}
	if len(statement) > maximumStatementBytes {
		return fmt.Errorf("statement exceeds %d bytes", maximumStatementBytes)
	}
	if !utf8.ValidString(statement) || strings.ContainsRune(statement, 0) {
		return fmt.Errorf("statement must be valid UTF-8 without NUL characters")
	}
	tokens, err := tokenizeStatement(statement)
	if err != nil {
		return err
	}
	keyword := leadingKeyword(tokens)
	if kind == statementKindRead && !readLeadingKeywords[keyword] {
		return fmt.Errorf("query runs only statements that start with %s; use execute for writes", describeKeywords(readLeadingKeywords))
	}
	if kind == statementKindWrite && !writeLeadingKeywords[keyword] {
		return fmt.Errorf("execute runs only statements that start with %s", describeKeywords(writeLeadingKeywords))
	}
	if err := validateSingleStatement(tokens); err != nil {
		return err
	}
	if err := validatePlaceholders(tokens, placeholderCount); err != nil {
		return err
	}
	if isSentAsProcedureCall(statement) {
		return fmt.Errorf("statement would be sent as a stored procedure call; separate its keywords with whitespace")
	}
	return nil
}

// validateSingleStatement rejects a semicolon before the end and every denied reserved keyword.
func validateSingleStatement(tokens []sqlToken) error {
	for index, token := range tokens {
		if token.kind == tokenSemicolon {
			for _, following := range tokens[index+1:] {
				if following.kind != tokenSemicolon {
					return fmt.Errorf("statement holds more than one statement; a semicolon may only end it")
				}
			}
			return nil
		}
		if token.kind != tokenWord {
			continue
		}
		keyword := strings.ToUpper(token.text)
		category, isDenied := deniedKeywords[keyword]
		if !isDenied {
			continue
		}
		if keyword == "USE" && index+1 < len(tokens) && tokens[index+1].kind == tokenWord && queryHintKeywords[strings.ToUpper(tokens[index+1].text)] {
			continue
		}
		return fmt.Errorf("statement uses the reserved keyword %s, which belongs to %s; the connector runs one SELECT, INSERT, UPDATE, DELETE, or MERGE", keyword, category)
	}
	return nil
}

// validatePlaceholders requires exactly @p1 through @pN and no other variable, because no statement can declare one.
func validatePlaceholders(tokens []sqlToken, placeholderCount int) error {
	referenced := map[int]bool{}
	for _, token := range tokens {
		if token.kind != tokenVariable || strings.HasPrefix(token.text, "@@") {
			continue
		}
		match := positionalParameterPattern.FindStringSubmatch(token.text)
		if match == nil {
			if strings.EqualFold(token.text[:min(2, len(token.text))], "@p") {
				return fmt.Errorf("statement references %s; write positional parameters as lowercase @p1 through @pN without leading zeros", token.text)
			}
			return fmt.Errorf("statement references the variable %s; only the positional parameters @p1 through @pN can be bound", token.text)
		}
		position, err := strconv.Atoi(match[1])
		if err != nil || position > placeholderCount {
			return fmt.Errorf("statement references %s but only %d parameters are bound", token.text, placeholderCount)
		}
		referenced[position] = true
	}
	for position := 1; position <= placeholderCount; position++ {
		if !referenced[position] {
			return fmt.Errorf("parameter @p%d is bound but the statement never references it", position)
		}
	}
	return nil
}

// leadingKeyword returns the first word after opening parentheses, upper-cased, or "" when the statement starts otherwise.
func leadingKeyword(tokens []sqlToken) string {
	for _, token := range tokens {
		switch token.kind {
		case tokenOpenParenthesis:
			continue
		case tokenWord:
			return strings.ToUpper(token.text)
		default:
			return ""
		}
	}
	return ""
}

// tokenizeStatement skips literals and comments; under the pinned QUOTED_IDENTIFIER ON, double quotes delimit identifiers.
func tokenizeStatement(statement string) ([]sqlToken, error) {
	var tokens []sqlToken
	remaining := statement
	for remaining != "" {
		character, width := utf8.DecodeRuneInString(remaining)
		switch {
		case unicode.IsSpace(character):
			remaining = remaining[width:]
		case strings.HasPrefix(remaining, "--"):
			newline := strings.IndexAny(remaining, "\r\n")
			if newline < 0 {
				return tokens, nil
			}
			remaining = remaining[newline:]
		case strings.HasPrefix(remaining, "/*"):
			end, err := blockCommentEnd(remaining)
			if err != nil {
				return nil, err
			}
			remaining = remaining[end:]
		case character == '\'':
			end, err := delimitedEnd(remaining, '\'', "string literal")
			if err != nil {
				return nil, err
			}
			remaining = remaining[end:]
		case character == '[':
			end, err := delimitedEnd(remaining, ']', "bracketed identifier")
			if err != nil {
				return nil, err
			}
			remaining = remaining[end:]
		case character == '"':
			end, err := delimitedEnd(remaining, '"', "quoted identifier")
			if err != nil {
				return nil, err
			}
			remaining = remaining[end:]
		case (character == 'N' || character == 'n') && strings.HasPrefix(remaining[width:], "'"):
			end, err := delimitedEnd(remaining[width:], '\'', "string literal")
			if err != nil {
				return nil, err
			}
			remaining = remaining[width+end:]
		case character == '@':
			end := identifierEnd(remaining, width)
			for end < len(remaining) && remaining[end] == '@' {
				end = identifierEnd(remaining, end+1)
			}
			tokens = append(tokens, sqlToken{kind: tokenVariable, text: remaining[:end]})
			remaining = remaining[end:]
		case unicode.IsLetter(character) || character == '_' || character == '#' || character == '$':
			end := identifierEnd(remaining, width)
			tokens = append(tokens, sqlToken{kind: tokenWord, text: remaining[:end]})
			remaining = remaining[end:]
		case unicode.IsDigit(character):
			end := identifierEnd(remaining, width)
			tokens = append(tokens, sqlToken{kind: tokenOther, text: remaining[:end]})
			remaining = remaining[end:]
		case character == ';':
			tokens = append(tokens, sqlToken{kind: tokenSemicolon, text: ";"})
			remaining = remaining[width:]
		case character == '(':
			tokens = append(tokens, sqlToken{kind: tokenOpenParenthesis, text: "("})
			remaining = remaining[width:]
		default:
			tokens = append(tokens, sqlToken{kind: tokenOther, text: remaining[:width]})
			remaining = remaining[width:]
		}
	}
	return tokens, nil
}

// identifierEnd returns the index after the identifier characters that start at offset.
func identifierEnd(text string, offset int) int {
	for offset < len(text) {
		character, width := utf8.DecodeRuneInString(text[offset:])
		if !unicode.IsLetter(character) && !unicode.IsDigit(character) && character != '_' && character != '#' && character != '$' && character != '@' {
			return offset
		}
		offset += width
	}
	return offset
}

// delimitedEnd returns the index after a literal or identifier whose closing delimiter is escaped by doubling.
func delimitedEnd(text string, closing byte, description string) (int, error) {
	for index := 1; index < len(text); index++ {
		if text[index] != closing {
			continue
		}
		if index+1 < len(text) && text[index+1] == closing {
			index++
			continue
		}
		return index + 1, nil
	}
	return 0, fmt.Errorf("statement has an unterminated %s", description)
}

// blockCommentEnd returns the index after a block comment, honoring T-SQL's nested comments.
func blockCommentEnd(text string) (int, error) {
	depth := 0
	for index := 0; index+1 < len(text); index++ {
		switch {
		case text[index] == '/' && text[index+1] == '*':
			depth++
			index++
		case text[index] == '*' && text[index+1] == '/':
			depth--
			index++
			if depth == 0 {
				return index + 1, nil
			}
		}
	}
	return 0, fmt.Errorf("statement has an unterminated block comment")
}

// isSentAsProcedureCall mirrors the driver's rule for treating bare text as a stored procedure name.
func isSentAsProcedureCall(statement string) bool {
	const (
		outside = iota
		text
		escaped
	)
	state := outside
	var previous, current rune
	for _, character := range statement {
		previous, current = current, character
		if state != escaped && strings.ContainsRune("\n\r';", character) {
			return false
		}
		switch state {
		case outside:
			switch {
			case character == '[':
				state = escaped
			case character == ']' && previous == ']':
				state = escaped
			case unicode.IsLetter(character) || character == '_' || character == '#':
				state = text
			case character == '.':
			default:
				return false
			}
		case text:
			switch {
			case character == '.':
				state = outside
			case character == '[' || character == '(' || unicode.IsSpace(character):
				return false
			}
		case escaped:
			if character == ']' {
				state = outside
			}
		}
	}
	upper := strings.ToUpper(statement)
	return upper != "RECONFIGURE" && upper != "SHUTDOWN" && upper != "CHECKPOINT" && upper != "COMMIT" && upper != "ROLLBACK"
}

func describeKeywords(keywords map[string]bool) string {
	names := make([]string, 0, len(keywords))
	for keyword := range keywords {
		names = append(names, keyword)
	}
	sort.Strings(names)
	if len(names) == 1 {
		return names[0]
	}
	return strings.Join(names[:len(names)-1], ", ") + ", or " + names[len(names)-1]
}

// encodeParameter converts one Go value to the driver value bound to an @pN placeholder, and its size.
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
		return encodeUnsignedParameter(uint64(value))
	case uint8:
		return int64(value), 8, nil
	case uint16:
		return int64(value), 8, nil
	case uint32:
		return int64(value), 8, nil
	case uint64:
		return encodeUnsignedParameter(value)
	case float32:
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return nil, 0, fmt.Errorf("SQL Server cannot store NaN or Infinity")
		}
		// The shortest float32 text, parsed as float64, avoids binary widening artifacts such as 0.10000000149.
		widened, err := strconv.ParseFloat(strconv.FormatFloat(float64(value), 'g', -1, 32), 64)
		return widened, 8, err
	case float64:
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return nil, 0, fmt.Errorf("SQL Server cannot store NaN or Infinity")
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
		return utc, 10, nil
	default:
		return nil, 0, fmt.Errorf("unsupported Go type %T; pass nil, a string, bool, integer, float, json.Number, json.RawMessage, []byte, or time.Time", parameter)
	}
}

// encodeUnsignedParameter binds an unsigned value as bigint, which cannot hold values above 2^63-1.
func encodeUnsignedParameter(value uint64) (driver.Value, int, error) {
	if value > math.MaxInt64 {
		return nil, 0, fmt.Errorf("SQL Server has no unsigned 64-bit type; pass a decimal string and CAST(@pN AS decimal(20, 0))")
	}
	return int64(value), 8, nil
}
