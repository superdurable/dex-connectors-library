// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// ConditionOperator is a COQL comparator. Zoho CRM decides which comparators a field type accepts;
// for example text fields take = != in and not in, and number, date, and datetime fields also take
// the ordering comparators.
type ConditionOperator string

const (
	// ConditionEquals matches a field equal to Value.
	ConditionEquals ConditionOperator = "="
	// ConditionNotEquals matches a field not equal to Value.
	ConditionNotEquals ConditionOperator = "!="
	// ConditionGreaterThan matches a number, date, or datetime field after Value.
	ConditionGreaterThan ConditionOperator = ">"
	// ConditionGreaterOrEqual matches a number, date, or datetime field at or after Value.
	ConditionGreaterOrEqual ConditionOperator = ">="
	// ConditionLessThan matches a number, date, or datetime field before Value.
	ConditionLessThan ConditionOperator = "<"
	// ConditionLessOrEqual matches a number, date, or datetime field at or before Value.
	ConditionLessOrEqual ConditionOperator = "<="
	// ConditionIn matches a field equal to any entry of a list Value.
	ConditionIn ConditionOperator = "in"
	// ConditionNotIn matches a field equal to no entry of a list Value.
	ConditionNotIn ConditionOperator = "not in"
	// ConditionIsNull matches an empty field and takes no Value.
	ConditionIsNull ConditionOperator = "is null"
	// ConditionIsNotNull matches a non-empty field and takes no Value.
	ConditionIsNotNull ConditionOperator = "is not null"
)

// COQLValueKind selects how a COQLValue is written into the query.
type COQLValueKind string

const (
	// COQLValueKindText writes Text as a quoted literal.
	COQLValueKindText COQLValueKind = "text"
	// COQLValueKindTextList writes Texts as a parenthesized list of quoted literals for in and not in.
	COQLValueKindTextList COQLValueKind = "textList"
	// COQLValueKindNumber writes Text, a plain decimal such as -12 or 1250.50, unquoted.
	COQLValueKindNumber COQLValueKind = "number"
	// COQLValueKindBoolean writes Boolean as true or false.
	COQLValueKindBoolean COQLValueKind = "boolean"
	// COQLValueKindDate writes Text, a YYYY-MM-DD date, quoted.
	COQLValueKindDate COQLValueKind = "date"
	// COQLValueKindDateTime writes Text, an instant in Zoho CRM's 2006-01-02T15:04:05+00:00 form, quoted.
	COQLValueKindDateTime COQLValueKind = "dateTime"
	// COQLValueKindRecordID writes Text, a record or user ID for a lookup or owner field, quoted.
	COQLValueKindRecordID COQLValueKind = "recordId"
	// COQLValueKindRecordIDList writes Texts as a list of quoted record or user IDs for in and not in.
	COQLValueKindRecordIDList COQLValueKind = "recordIdList"
)

const (
	// MaxConditions is the most conditions one findRecords call combines.
	MaxConditions = 10
	// MaxConditionListValues is the most entries of an in or not in list, Zoho CRM's COQL limit.
	MaxConditionListValues = 100
	// MaxConditionTextRunes is the longest text value a condition accepts.
	MaxConditionTextRunes = 1000
)

var (
	coqlNumberPattern = regexp.MustCompile(`^-?[0-9]{1,18}(?:\.[0-9]{1,9})?$`)
	coqlDatePattern   = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)
)

// COQLValue is one typed value of a condition. Build it with COQLText, COQLTextList, COQLNumber,
// COQLBoolean, COQLDate, COQLDateTime, COQLRecordID, or COQLRecordIDList, so a value can never
// change the query around it.
type COQLValue struct {
	// Kind selects the rendering.
	Kind COQLValueKind `json:"kind"`
	// Text holds the value for every kind except the lists and boolean.
	Text string `json:"text,omitempty"`
	// Texts holds the entries of a list.
	Texts []string `json:"texts,omitempty"`
	// Boolean holds the value of a boolean.
	Boolean bool `json:"boolean,omitempty"`
}

// COQLText is a text value such as an email address or account name. Text with an apostrophe or
// backslash is rejected as a defect, because Zoho CRM documents no escape for them in COQL.
func COQLText(text string) COQLValue { return COQLValue{Kind: COQLValueKindText, Text: text} }

// COQLTextList is a list of text values for in and not in, such as picklist values.
func COQLTextList(texts []string) COQLValue {
	return COQLValue{Kind: COQLValueKindTextList, Texts: append([]string(nil), texts...)}
}

// COQLNumber is a plain decimal written as text, such as 50000 or 1250.50, so currency keeps its digits.
func COQLNumber(decimal string) COQLValue { return COQLValue{Kind: COQLValueKindNumber, Text: decimal} }

// COQLBoolean is a checkbox value.
func COQLBoolean(value bool) COQLValue { return COQLValue{Kind: COQLValueKindBoolean, Boolean: value} }

// COQLDate is the calendar date of value as written in its own location.
func COQLDate(value time.Time) COQLValue {
	return COQLValue{Kind: COQLValueKindDate, Text: value.Format(time.DateOnly)}
}

// COQLDateTime is an instant truncated to whole seconds, written with a +00:00 offset.
func COQLDateTime(value time.Time) COQLValue {
	return COQLValue{Kind: COQLValueKindDateTime, Text: formatZohoTime(value)}
}

// COQLRecordID is the ID of a record or user, compared with a lookup or owner field such as Account_Name or Owner.
func COQLRecordID(recordID string) COQLValue {
	return COQLValue{Kind: COQLValueKindRecordID, Text: recordID}
}

// COQLRecordIDList is a list of record or user IDs for in and not in.
func COQLRecordIDList(recordIDs []string) COQLValue {
	return COQLValue{Kind: COQLValueKindRecordIDList, Texts: append([]string(nil), recordIDs...)}
}

// RecordCondition is one COQL criterion: a field path, a comparator, and a typed value.
type RecordCondition struct {
	// Field is a field API name, such as Email, or a lookup path with at most two joins, such as Account_Name.Account_Name.
	Field string `json:"field"`
	// Operator is the comparator.
	Operator ConditionOperator `json:"operator"`
	// Value is the typed value; it is nil for is null and is not null, and a list for in and not in.
	Value *COQLValue `json:"value,omitempty"`
}

// RecordSort orders results by one field; record ID breaks ties in ascending order.
type RecordSort struct {
	// Field is the field API name to order by, such as Modified_Time.
	Field string `json:"field"`
	// IsDescending orders from the highest value instead of the lowest.
	IsDescending bool `json:"isDescending,omitempty"`
}

// renderConditions joins the conditions with and, nesting pairs as Zoho CRM's examples do: ((A and B) and C).
func renderConditions(conditions []RecordCondition) (string, error) {
	if len(conditions) == 0 || len(conditions) > MaxConditions {
		return "", fmt.Errorf("conditions must hold 1 to %d conditions", MaxConditions)
	}
	rendered := ""
	for index, condition := range conditions {
		criterion, err := renderCondition(condition)
		if err != nil {
			return "", fmt.Errorf("condition %d: %w", index+1, err)
		}
		if index == 0 {
			rendered = criterion
			continue
		}
		rendered = "(" + rendered + " and " + criterion + ")"
	}
	return rendered, nil
}

func renderCondition(condition RecordCondition) (string, error) {
	if !isFieldPath(condition.Field) {
		return "", errors.New("field must be a Zoho CRM field API name such as Email or Account_Name.Account_Name")
	}
	switch condition.Operator {
	case ConditionIsNull, ConditionIsNotNull:
		if condition.Value != nil {
			return "", fmt.Errorf("%s takes no value", condition.Operator)
		}
		return condition.Field + " " + string(condition.Operator), nil
	case ConditionIn, ConditionNotIn:
		if condition.Value == nil || (condition.Value.Kind != COQLValueKindTextList && condition.Value.Kind != COQLValueKindRecordIDList) {
			return "", fmt.Errorf("%s needs a COQLTextList or COQLRecordIDList value", condition.Operator)
		}
	case ConditionEquals, ConditionNotEquals:
		if condition.Value == nil || condition.Value.Kind == COQLValueKindTextList || condition.Value.Kind == COQLValueKindRecordIDList {
			return "", fmt.Errorf("%s needs one value, not a list", condition.Operator)
		}
	case ConditionGreaterThan, ConditionGreaterOrEqual, ConditionLessThan, ConditionLessOrEqual:
		if condition.Value == nil || !isOrderedKind(condition.Value.Kind) {
			return "", fmt.Errorf("%s needs a number, date, or datetime value", condition.Operator)
		}
	default:
		return "", errors.New("operator must be one of = != > >= < <= in, not in, is null, or is not null")
	}
	literal, err := renderCOQLValue(*condition.Value)
	if err != nil {
		return "", err
	}
	return condition.Field + " " + string(condition.Operator) + " " + literal, nil
}

func isOrderedKind(kind COQLValueKind) bool {
	return kind == COQLValueKindNumber || kind == COQLValueKindDate || kind == COQLValueKindDateTime
}

func renderCOQLValue(value COQLValue) (string, error) {
	switch value.Kind {
	case COQLValueKindText:
		return quoteCOQLText(value.Text)
	case COQLValueKindTextList, COQLValueKindRecordIDList:
		return renderCOQLList(value)
	case COQLValueKindNumber:
		if !coqlNumberPattern.MatchString(value.Text) {
			return "", errors.New("a number value must be a plain decimal such as 50000 or 1250.50")
		}
		return value.Text, nil
	case COQLValueKindBoolean:
		return strconv.FormatBool(value.Boolean), nil
	case COQLValueKindDate:
		if _, err := time.Parse(time.DateOnly, value.Text); err != nil || !coqlDatePattern.MatchString(value.Text) {
			return "", errors.New("a date value must be YYYY-MM-DD")
		}
		return "'" + value.Text + "'", nil
	case COQLValueKindDateTime:
		if _, err := time.Parse(zohoTimeLayout, value.Text); err != nil {
			return "", errors.New("a datetime value must be built with COQLDateTime")
		}
		return "'" + value.Text + "'", nil
	case COQLValueKindRecordID:
		if !isRecordID(value.Text) {
			return "", errors.New("a record ID value must be numeric")
		}
		return "'" + value.Text + "'", nil
	default:
		return "", errors.New("value kind is not supported")
	}
}

func renderCOQLList(value COQLValue) (string, error) {
	if len(value.Texts) == 0 || len(value.Texts) > MaxConditionListValues {
		return "", fmt.Errorf("a list value must hold 1 to %d entries", MaxConditionListValues)
	}
	entries := make([]string, 0, len(value.Texts))
	for _, text := range value.Texts {
		if value.Kind == COQLValueKindRecordIDList && !isRecordID(text) {
			return "", errors.New("a record ID list may hold only numeric IDs")
		}
		quoted, err := quoteCOQLText(text)
		if err != nil {
			return "", err
		}
		entries = append(entries, quoted)
	}
	return "(" + strings.Join(entries, ", ") + ")", nil
}

// quoteCOQLText quotes text that cannot leave its literal: no apostrophe, backslash, or control character.
func quoteCOQLText(text string) (string, error) {
	if text == "" || !utf8.ValidString(text) || utf8.RuneCountInString(text) > MaxConditionTextRunes {
		return "", fmt.Errorf("a text value must be 1 to %d characters of valid UTF-8", MaxConditionTextRunes)
	}
	if strings.ContainsAny(text, `'\`) || strings.IndexFunc(text, unicode.IsControl) >= 0 {
		return "", errors.New("a text value cannot contain an apostrophe, a backslash, or a control character, which COQL cannot quote safely")
	}
	return "'" + text + "'", nil
}

// renderSelectQuery writes one COQL select; id is always returned, so it is never selected twice.
func renderSelectQuery(module string, fieldNames []string, where string, orderBy string, offset int, limit int) string {
	selected := make([]string, 0, len(fieldNames))
	for _, name := range fieldNames {
		if name != "id" {
			selected = append(selected, name)
		}
	}
	if len(selected) == 0 {
		selected = []string{"id"}
	}
	return "select " + strings.Join(selected, ", ") + " from " + module + " where " + where +
		" order by " + orderBy + " limit " + strconv.Itoa(offset) + ", " + strconv.Itoa(limit)
}

// renderOrderBy orders by the sort field and then record ID, so equal values page in a stable order.
func renderOrderBy(sort *RecordSort) (string, error) {
	if sort == nil || sort.Field == "id" {
		direction := "asc"
		if sort != nil && sort.IsDescending {
			direction = "desc"
		}
		return "id " + direction, nil
	}
	if !isFieldPath(sort.Field) {
		return "", errors.New("sort field must be a Zoho CRM field API name such as Modified_Time")
	}
	direction := "asc"
	if sort.IsDescending {
		direction = "desc"
	}
	return sort.Field + " " + direction + ", id asc", nil
}
