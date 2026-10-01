// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

// SOQLValueKind selects how a bound value is rendered into a SOQL statement.
type SOQLValueKind string

const (
	// SOQLValueKindString renders Text as a quoted, escaped string literal.
	SOQLValueKindString SOQLValueKind = "string"
	// SOQLValueKindStringList renders Texts as a parenthesized list of string
	// literals for IN and NOT IN; the list must not be empty.
	SOQLValueKindStringList SOQLValueKind = "stringList"
	// SOQLValueKindNumber renders Text, a plain decimal such as -12 or 1250.50, unquoted.
	SOQLValueKindNumber SOQLValueKind = "number"
	// SOQLValueKindBoolean renders Boolean as TRUE or FALSE.
	SOQLValueKindBoolean SOQLValueKind = "boolean"
	// SOQLValueKindNull renders NULL.
	SOQLValueKindNull SOQLValueKind = "null"
	// SOQLValueKindDate renders Text, a YYYY-MM-DD date, as an unquoted date literal.
	SOQLValueKindDate SOQLValueKind = "date"
	// SOQLValueKindDateTime renders Text, an RFC 3339 timestamp, as an
	// unquoted UTC dateTime literal truncated to whole seconds.
	SOQLValueKindDateTime SOQLValueKind = "dateTime"
	// SOQLValueKindLikeContains renders Text as a LIKE pattern that matches it
	// anywhere; % and _ in Text match themselves.
	SOQLValueKindLikeContains SOQLValueKind = "likeContains"
	// SOQLValueKindLikeStartsWith renders Text as a LIKE pattern that matches
	// values starting with it; % and _ in Text match themselves.
	SOQLValueKindLikeStartsWith SOQLValueKind = "likeStartsWith"
	// SOQLValueKindIdentifier renders Text, an object or field API name or a
	// dotted relationship path such as Account.Name, unquoted.
	SOQLValueKindIdentifier SOQLValueKind = "identifier"
)

const (
	maxSOQLBindings     = 100
	maxStringListValues = 1000
	maxNumberCharacters = 40
	soqlDateTimeLayout  = "2006-01-02T15:04:05Z"
)

var (
	bindingNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)
	numberPattern      = regexp.MustCompile(`^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?$`)
	// identifierPattern allows a relationship path of at most five names, SOQL's relationship depth.
	identifierPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*){0,4}$`)
)

// SOQLValue is one typed value bound to a :name placeholder in a
// QueryRecordsInput SOQL statement. The connector renders it as an escaped
// literal, so bound text can never change the statement's structure. Build it
// with a constructor such as SOQLString; the exported fields exist so a value
// can travel in durable Step input.
type SOQLValue struct {
	// Kind selects the rendering.
	Kind SOQLValueKind `json:"kind"`
	// Text holds the value for every kind except stringList, boolean, and null.
	Text string `json:"text,omitempty"`
	// Texts holds the values of a stringList.
	Texts []string `json:"texts,omitempty"`
	// Boolean holds the value of a boolean.
	Boolean bool `json:"boolean,omitempty"`
}

// SOQLString binds text as a string literal, such as an email address or name.
func SOQLString(text string) SOQLValue { return SOQLValue{Kind: SOQLValueKindString, Text: text} }

// SOQLStringList binds texts as an IN list, such as a set of record IDs.
func SOQLStringList(texts []string) SOQLValue {
	return SOQLValue{Kind: SOQLValueKindStringList, Texts: append([]string(nil), texts...)}
}

// SOQLNumber binds a plain decimal written as text, such as 50000 or 1250.50,
// so currency values keep their exact digits.
func SOQLNumber(decimal string) SOQLValue { return SOQLValue{Kind: SOQLValueKindNumber, Text: decimal} }

// SOQLBoolean binds TRUE or FALSE.
func SOQLBoolean(value bool) SOQLValue { return SOQLValue{Kind: SOQLValueKindBoolean, Boolean: value} }

// SOQLNull binds NULL, as in WHERE Email = :email with a missing email.
func SOQLNull() SOQLValue { return SOQLValue{Kind: SOQLValueKindNull} }

// SOQLDate binds the calendar date of value in value's own location, for Date fields such as CloseDate.
func SOQLDate(value time.Time) SOQLValue {
	return SOQLValue{Kind: SOQLValueKindDate, Text: value.Format(time.DateOnly)}
}

// SOQLDateTime binds value as a UTC dateTime literal truncated to whole
// seconds, for DateTime fields such as LastModifiedDate.
func SOQLDateTime(value time.Time) SOQLValue {
	return SOQLValue{Kind: SOQLValueKindDateTime, Text: value.UTC().Format(time.RFC3339)}
}

// SOQLLikeContains binds a LIKE pattern matching text anywhere in the field.
func SOQLLikeContains(text string) SOQLValue {
	return SOQLValue{Kind: SOQLValueKindLikeContains, Text: text}
}

// SOQLLikeStartsWith binds a LIKE pattern matching field values that start with text.
func SOQLLikeStartsWith(text string) SOQLValue {
	return SOQLValue{Kind: SOQLValueKindLikeStartsWith, Text: text}
}

// SOQLIdentifier binds an object or field API name, such as a configured
// Contact or ERP_Id__c, or a relationship path such as Account.Name. The name
// is validated, never quoted, so it cannot carry an expression.
func SOQLIdentifier(apiName string) SOQLValue {
	return SOQLValue{Kind: SOQLValueKindIdentifier, Text: apiName}
}

// renderSOQL replaces :name placeholders outside string literals; errors name a binding, never its value.
func renderSOQL(statement string, bindings map[string]SOQLValue) (string, error) {
	if strings.TrimSpace(statement) == "" {
		return "", fmt.Errorf("soql is required")
	}
	if !utf8.ValidString(statement) {
		return "", fmt.Errorf("soql must be valid UTF-8")
	}
	if len(bindings) > maxSOQLBindings {
		return "", fmt.Errorf("soql accepts at most %d bindings", maxSOQLBindings)
	}
	for name := range bindings {
		if !bindingNamePattern.MatchString(name) {
			return "", fmt.Errorf("binding names must match %s", bindingNamePattern.String())
		}
	}
	var rendered strings.Builder
	usedBindings := make(map[string]bool, len(bindings))
	isInStringLiteral := false
	for index := 0; index < len(statement); index++ {
		character := statement[index]
		switch {
		case isInStringLiteral && character == '\\' && index+1 < len(statement):
			rendered.WriteByte(character)
			index++
			rendered.WriteByte(statement[index])
		case character == '\'':
			isInStringLiteral = !isInStringLiteral
			rendered.WriteByte(character)
		case !isInStringLiteral && character == ':' && index+1 < len(statement) && isBindingNameStart(statement[index+1]):
			end := index + 1
			for end < len(statement) && isBindingNameCharacter(statement[end]) {
				end++
			}
			name := statement[index+1 : end]
			value, isBound := bindings[name]
			if !isBound {
				return "", fmt.Errorf("placeholder :%s has no binding", name)
			}
			literal, err := value.render()
			if err != nil {
				return "", fmt.Errorf("binding %s: %w", name, err)
			}
			usedBindings[name] = true
			rendered.WriteString(literal)
			index = end - 1
		default:
			rendered.WriteByte(character)
		}
	}
	if isInStringLiteral {
		return "", fmt.Errorf("soql has an unterminated string literal")
	}
	unusedBindings := make([]string, 0)
	for name := range bindings {
		if !usedBindings[name] {
			unusedBindings = append(unusedBindings, name)
		}
	}
	if len(unusedBindings) > 0 {
		sort.Strings(unusedBindings)
		return "", fmt.Errorf("bindings %s have no placeholder", strings.Join(unusedBindings, ", "))
	}
	return rendered.String(), nil
}

// render returns the SOQL literal for value, or an error that never repeats it.
func (value SOQLValue) render() (string, error) {
	switch value.Kind {
	case SOQLValueKindString:
		return quoteSOQLString(value.Text, "", "")
	case SOQLValueKindLikeContains:
		return quoteSOQLString(value.Text, "%", "%")
	case SOQLValueKindLikeStartsWith:
		return quoteSOQLString(value.Text, "", "%")
	case SOQLValueKindStringList:
		if len(value.Texts) == 0 || len(value.Texts) > maxStringListValues {
			return "", fmt.Errorf("stringList must hold from 1 to %d values", maxStringListValues)
		}
		literals := make([]string, 0, len(value.Texts))
		for _, text := range value.Texts {
			literal, err := quoteSOQLString(text, "", "")
			if err != nil {
				return "", err
			}
			literals = append(literals, literal)
		}
		return "(" + strings.Join(literals, ", ") + ")", nil
	case SOQLValueKindNumber:
		if len(value.Text) > maxNumberCharacters || !numberPattern.MatchString(value.Text) {
			return "", fmt.Errorf("number must be a plain decimal such as -12 or 1250.50")
		}
		return value.Text, nil
	case SOQLValueKindBoolean:
		if value.Boolean {
			return "TRUE", nil
		}
		return "FALSE", nil
	case SOQLValueKindNull:
		return "NULL", nil
	case SOQLValueKindDate:
		parsed, err := time.Parse(time.DateOnly, value.Text)
		if err != nil {
			return "", fmt.Errorf("date must be YYYY-MM-DD")
		}
		return parsed.Format(time.DateOnly), nil
	case SOQLValueKindDateTime:
		parsed, err := time.Parse(time.RFC3339, value.Text)
		if err != nil {
			return "", fmt.Errorf("dateTime must be an RFC 3339 timestamp")
		}
		return parsed.UTC().Format(soqlDateTimeLayout), nil
	case SOQLValueKindIdentifier:
		if len(value.Text) > maxAPINameBytes || !identifierPattern.MatchString(value.Text) {
			return "", fmt.Errorf("identifier must be an API name or a dotted relationship path")
		}
		return value.Text, nil
	default:
		return "", fmt.Errorf("kind is not a supported SOQL value kind")
	}
}

// quoteSOQLString escapes text as a SOQL string literal between unescaped LIKE wildcards prefix and suffix.
func quoteSOQLString(text string, prefix string, suffix string) (string, error) {
	if !utf8.ValidString(text) {
		return "", fmt.Errorf("text must be valid UTF-8")
	}
	isLikePattern := prefix != "" || suffix != ""
	var quoted strings.Builder
	quoted.WriteString("'" + prefix)
	for _, character := range text {
		switch character {
		case '\\':
			quoted.WriteString(`\\`)
		case '\'':
			quoted.WriteString(`\'`)
		case '"':
			quoted.WriteString(`\"`)
		case '\n':
			quoted.WriteString(`\n`)
		case '\r':
			quoted.WriteString(`\r`)
		case '\t':
			quoted.WriteString(`\t`)
		case '\b':
			quoted.WriteString(`\b`)
		case '\f':
			quoted.WriteString(`\f`)
		case '%', '_':
			if isLikePattern {
				quoted.WriteByte('\\')
			}
			quoted.WriteRune(character)
		default:
			if character < 0x20 || character == 0x7f {
				return "", fmt.Errorf("text contains a control character SOQL cannot escape")
			}
			quoted.WriteRune(character)
		}
	}
	quoted.WriteString(suffix + "'")
	return quoted.String(), nil
}

func isBindingNameStart(character byte) bool {
	return character == '_' || (character >= 'A' && character <= 'Z') || (character >= 'a' && character <= 'z')
}

func isBindingNameCharacter(character byte) bool {
	return isBindingNameStart(character) || (character >= '0' && character <= '9')
}
