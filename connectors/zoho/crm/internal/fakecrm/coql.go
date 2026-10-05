// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package fakecrm

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var selectQueryPattern = regexp.MustCompile(`^select (.+) from ([A-Za-z][A-Za-z0-9_]*) where (.+) order by (.+) limit ([0-9]+), ([0-9]+)$`)

// selectQuery is the subset of COQL the connector writes.
type selectQuery struct {
	fields    []string
	module    string
	criterion criterion
	orderBy   []orderTerm
	offset    int
	limit     int
}

type orderTerm struct {
	field        string
	isDescending bool
}

// criterion is a parsed where clause: a group of criteria joined by and/or, or one comparison.
type criterion struct {
	children []criterion
	joiner   string
	field    string
	operator string
	values   []literal
}

type literal struct {
	text     string
	isQuoted bool
}

func parseSelectQuery(query string) (selectQuery, error) {
	matches := selectQueryPattern.FindStringSubmatch(query)
	if matches == nil {
		return selectQuery{}, errors.New("not a select query")
	}
	parsed := selectQuery{module: matches[2]}
	for _, field := range strings.Split(matches[1], ",") {
		parsed.fields = append(parsed.fields, strings.TrimSpace(field))
	}
	parser := &criterionParser{tokens: tokenizeCriteria(matches[3])}
	where, err := parser.parseGroup()
	if err != nil || parser.position != len(parser.tokens) {
		return selectQuery{}, errors.New("invalid where clause")
	}
	parsed.criterion = where
	for _, term := range strings.Split(matches[4], ",") {
		words := strings.Fields(term)
		if len(words) != 2 || (words[1] != "asc" && words[1] != "desc") {
			return selectQuery{}, errors.New("invalid order by")
		}
		parsed.orderBy = append(parsed.orderBy, orderTerm{field: words[0], isDescending: words[1] == "desc"})
	}
	parsed.offset, _ = strconv.Atoi(matches[5])
	parsed.limit, _ = strconv.Atoi(matches[6])
	return parsed, nil
}

// tokenizeCriteria splits on spaces, commas, and parentheses, keeping quoted literals whole.
func tokenizeCriteria(text string) []string {
	var tokens []string
	for index := 0; index < len(text); {
		switch character := text[index]; {
		case character == ' ':
			index++
		case character == '(' || character == ')' || character == ',':
			tokens = append(tokens, string(character))
			index++
		case character == '\'':
			end := strings.IndexByte(text[index+1:], '\'')
			if end < 0 {
				return append(tokens, text[index:])
			}
			tokens = append(tokens, text[index:index+end+2])
			index += end + 2
		default:
			end := strings.IndexAny(text[index:], " (),")
			if end < 0 {
				end = len(text) - index
			}
			tokens = append(tokens, text[index:index+end])
			index += end
		}
	}
	return tokens
}

type criterionParser struct {
	tokens   []string
	position int
}

func (parser *criterionParser) parseGroup() (criterion, error) {
	first, err := parser.parseTerm()
	if err != nil {
		return criterion{}, err
	}
	group := criterion{children: []criterion{first}}
	for parser.peek() == "and" || parser.peek() == "or" {
		joiner := parser.next()
		if group.joiner != "" && group.joiner != joiner {
			return criterion{}, errors.New("mixed and/or without parentheses")
		}
		group.joiner = joiner
		next, err := parser.parseTerm()
		if err != nil {
			return criterion{}, err
		}
		group.children = append(group.children, next)
	}
	if len(group.children) == 1 {
		return first, nil
	}
	return group, nil
}

func (parser *criterionParser) parseTerm() (criterion, error) {
	if parser.peek() == "(" {
		parser.next()
		group, err := parser.parseGroup()
		if err != nil || parser.next() != ")" {
			return criterion{}, errors.New("unbalanced parentheses")
		}
		return group, nil
	}
	comparison := criterion{field: parser.next()}
	switch operator := parser.next(); operator {
	case "=", "!=", ">", ">=", "<", "<=":
		comparison.operator = operator
		comparison.values = []literal{parseLiteral(parser.next())}
	case "in":
		comparison.operator = "in"
		comparison.values = parser.parseList()
	case "not":
		if parser.next() != "in" {
			return criterion{}, errors.New("unknown operator")
		}
		comparison.operator = "not in"
		comparison.values = parser.parseList()
	case "is":
		comparison.operator = "is null"
		if parser.peek() == "not" {
			parser.next()
			comparison.operator = "is not null"
		}
		if parser.next() != "null" {
			return criterion{}, errors.New("unknown operator")
		}
	default:
		return criterion{}, errors.New("unknown operator")
	}
	return comparison, nil
}

func (parser *criterionParser) parseList() []literal {
	var values []literal
	if parser.next() != "(" {
		return nil
	}
	for parser.peek() != ")" && parser.peek() != "" {
		if token := parser.next(); token != "," {
			values = append(values, parseLiteral(token))
		}
	}
	parser.next()
	return values
}

func (parser *criterionParser) peek() string {
	if parser.position >= len(parser.tokens) {
		return ""
	}
	return parser.tokens[parser.position]
}

func (parser *criterionParser) next() string {
	token := parser.peek()
	parser.position++
	return token
}

func parseLiteral(token string) literal {
	if len(token) >= 2 && strings.HasPrefix(token, "'") && strings.HasSuffix(token, "'") {
		return literal{text: token[1 : len(token)-1], isQuoted: true}
	}
	return literal{text: token}
}

// matches evaluates the criterion against one stored record; text compares without regard to case.
func (rule criterion) matches(record map[string]any) bool {
	if len(rule.children) != 0 {
		for _, child := range rule.children {
			isMatched := child.matches(record)
			if rule.joiner == "or" && isMatched {
				return true
			}
			if rule.joiner != "or" && !isMatched {
				return false
			}
		}
		return rule.joiner != "or"
	}
	value, isPresent := comparableValue(record, rule.field)
	switch rule.operator {
	case "is null":
		return !isPresent
	case "is not null":
		return isPresent
	case "in", "not in":
		isMember := false
		for _, candidate := range rule.values {
			isMember = isMember || (isPresent && compareValues(value, candidate) == 0)
		}
		return isMember == (rule.operator == "in")
	}
	if !isPresent {
		return rule.operator == "!="
	}
	comparison := compareValues(value, rule.values[0])
	switch rule.operator {
	case "=":
		return comparison == 0
	case "!=":
		return comparison != 0
	case ">":
		return comparison > 0
	case ">=":
		return comparison >= 0
	case "<":
		return comparison < 0
	default:
		return comparison <= 0
	}
}

// comparableValue returns a field's text form: a lookup's id, a number's digits, or the text itself.
func comparableValue(record map[string]any, field string) (string, bool) {
	switch value := record[field].(type) {
	case nil:
		return "", false
	case string:
		return value, value != ""
	case float64:
		return strconv.FormatFloat(value, 'f', -1, 64), true
	case bool:
		return strconv.FormatBool(value), true
	case map[string]any:
		recordID, isText := value["id"].(string)
		return recordID, isText
	default:
		return "", false
	}
}

func compareValues(value string, candidate literal) int {
	if left, leftErr := time.Parse(time.RFC3339, value); leftErr == nil {
		if right, rightErr := time.Parse(time.RFC3339, candidate.text); rightErr == nil {
			return left.Compare(right)
		}
	}
	if isDigits(value) && isDigits(candidate.text) {
		return compareDecimalIDs(value, candidate.text)
	}
	if left, leftErr := strconv.ParseFloat(value, 64); leftErr == nil && !candidate.isQuoted {
		if right, rightErr := strconv.ParseFloat(candidate.text, 64); rightErr == nil {
			switch {
			case left < right:
				return -1
			case left > right:
				return 1
			}
			return 0
		}
	}
	return strings.Compare(strings.ToLower(value), strings.ToLower(candidate.text))
}

func isDigits(text string) bool {
	if text == "" {
		return false
	}
	for _, character := range text {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
}

func compareDecimalIDs(left string, right string) int {
	left, right = strings.TrimLeft(left, "0"), strings.TrimLeft(right, "0")
	if len(left) != len(right) {
		return len(left) - len(right)
	}
	return strings.Compare(left, right)
}
