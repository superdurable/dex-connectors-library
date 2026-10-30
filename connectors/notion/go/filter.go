// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// FilterCondition is a Notion filter condition, as Notion's API spells it.
type FilterCondition string

const (
	// FilterEquals matches an exact value.
	FilterEquals FilterCondition = "equals"
	// FilterDoesNotEqual matches every other value.
	FilterDoesNotEqual FilterCondition = "does_not_equal"
	// FilterContains matches a substring, an option, a person, or a related page.
	FilterContains FilterCondition = "contains"
	// FilterDoesNotContain is the negation of FilterContains.
	FilterDoesNotContain FilterCondition = "does_not_contain"
	// FilterStartsWith matches a text prefix.
	FilterStartsWith FilterCondition = "starts_with"
	// FilterEndsWith matches a text suffix.
	FilterEndsWith FilterCondition = "ends_with"
	// FilterIsEmpty matches an empty value and takes no value.
	FilterIsEmpty FilterCondition = "is_empty"
	// FilterIsNotEmpty matches a non-empty value and takes no value.
	FilterIsNotEmpty FilterCondition = "is_not_empty"
	// FilterGreaterThan compares numbers.
	FilterGreaterThan FilterCondition = "greater_than"
	// FilterLessThan compares numbers.
	FilterLessThan FilterCondition = "less_than"
	// FilterGreaterThanOrEqualTo compares numbers.
	FilterGreaterThanOrEqualTo FilterCondition = "greater_than_or_equal_to"
	// FilterLessThanOrEqualTo compares numbers.
	FilterLessThanOrEqualTo FilterCondition = "less_than_or_equal_to"
	// FilterBefore matches dates before Date.
	FilterBefore FilterCondition = "before"
	// FilterAfter matches dates after Date.
	FilterAfter FilterCondition = "after"
	// FilterOnOrBefore matches dates on or before Date.
	FilterOnOrBefore FilterCondition = "on_or_before"
	// FilterOnOrAfter matches dates on or after Date.
	FilterOnOrAfter FilterCondition = "on_or_after"
	// FilterPastWeek matches dates in the past week and takes no value.
	FilterPastWeek FilterCondition = "past_week"
	// FilterPastMonth matches dates in the past month and takes no value.
	FilterPastMonth FilterCondition = "past_month"
	// FilterPastYear matches dates in the past year and takes no value.
	FilterPastYear FilterCondition = "past_year"
	// FilterNextWeek matches dates in the next week and takes no value.
	FilterNextWeek FilterCondition = "next_week"
	// FilterNextMonth matches dates in the next month and takes no value.
	FilterNextMonth FilterCondition = "next_month"
	// FilterNextYear matches dates in the next year and takes no value.
	FilterNextYear FilterCondition = "next_year"
	// FilterThisWeek matches dates in the current week and takes no value.
	FilterThisWeek FilterCondition = "this_week"
)

// TimestampKind names a page timestamp that a filter or sort can use without a property.
type TimestampKind string

const (
	// TimestampCreatedTime is the page's creation time.
	TimestampCreatedTime TimestampKind = "created_time"
	// TimestampLastEditedTime is the page's last edit time.
	TimestampLastEditedTime TimestampKind = "last_edited_time"
)

// SortDirection orders query results.
type SortDirection string

const (
	// SortAscending orders from the smallest or earliest value.
	SortAscending SortDirection = "ascending"
	// SortDescending orders from the largest or latest value.
	SortDescending SortDirection = "descending"
)

const (
	// MaximumFilterConditions bounds the leaf conditions in one query filter.
	MaximumFilterConditions = 100
	// MaximumSorts bounds the sort criteria of one query.
	MaximumSorts = 10
	// maximumFilterNesting is Notion's limit for compound filters inside compound filters.
	maximumFilterNesting   = 2
	maximumFilterTextRunes = 2000
)

// QueryFilter is a typed Notion data source filter. Set exactly one of
// Property, Timestamp, And, and Or.
//
// A property condition names the property, its Type, a Condition the type
// supports, and the one value field the condition takes: Text for title,
// rich_text, url, email, phone_number, select, status, and multi_select;
// Number for number and unique_id; Checkbox for checkbox; Date for date; and
// ID for people and relation. FilterIsEmpty, FilterIsNotEmpty, and the
// relative date conditions such as FilterPastWeek take no value. A timestamp
// condition sets Timestamp, a date Condition, and Date. And and Or combine up to
// two levels of nested filters. An unsupported combination selects defect
// before any request is sent.
type QueryFilter struct {
	// Property is the property name or ID that the condition reads.
	Property string `json:"property,omitempty"`
	// Type is the property's Notion type, which selects the filter's shape.
	Type PropertyType `json:"type,omitempty"`
	// Timestamp filters on the page's creation or last edit time instead of a property.
	Timestamp TimestampKind `json:"timestamp,omitempty"`
	// Condition is the comparison.
	Condition FilterCondition `json:"condition,omitempty"`
	// Text is a text or option-name value.
	Text string `json:"text,omitempty"`
	// Number is a number value.
	Number *float64 `json:"number,omitempty"`
	// Checkbox is a checkbox value.
	Checkbox *bool `json:"checkbox,omitempty"`
	// Date is a YYYY-MM-DD date or RFC 3339 date-time value.
	Date string `json:"date,omitempty"`
	// ID is a Notion user ID for people or a page ID for relation.
	ID string `json:"id,omitempty"`
	// And matches pages that match every filter.
	And []QueryFilter `json:"and,omitempty"`
	// Or matches pages that match any filter.
	Or []QueryFilter `json:"or,omitempty"`
}

// QuerySort orders query results by a property or a timestamp. Set exactly
// one of Property and Timestamp.
type QuerySort struct {
	// Property is the property name or ID to sort by.
	Property string `json:"property,omitempty"`
	// Timestamp sorts by the page's creation or last edit time.
	Timestamp TimestampKind `json:"timestamp,omitempty"`
	// Direction is ascending or descending; blank is ascending.
	Direction SortDirection `json:"direction,omitempty"`
}

// AllOf returns a filter that matches pages matching every filter.
func AllOf(filters ...QueryFilter) QueryFilter { return QueryFilter{And: filters} }

// AnyOf returns a filter that matches pages matching any filter.
func AnyOf(filters ...QueryFilter) QueryFilter { return QueryFilter{Or: filters} }

type filterValueKind uint8

const (
	filterValueNone filterValueKind = iota + 1
	filterValueRelative
	filterValueText
	filterValueNumber
	filterValueCheckbox
	filterValueDate
	filterValueID
)

var (
	textConditions = map[FilterCondition]filterValueKind{
		FilterEquals: filterValueText, FilterDoesNotEqual: filterValueText, FilterContains: filterValueText,
		FilterDoesNotContain: filterValueText, FilterStartsWith: filterValueText, FilterEndsWith: filterValueText,
		FilterIsEmpty: filterValueNone, FilterIsNotEmpty: filterValueNone,
	}
	numberConditions = map[FilterCondition]filterValueKind{
		FilterEquals: filterValueNumber, FilterDoesNotEqual: filterValueNumber, FilterGreaterThan: filterValueNumber,
		FilterLessThan: filterValueNumber, FilterGreaterThanOrEqualTo: filterValueNumber, FilterLessThanOrEqualTo: filterValueNumber,
		FilterIsEmpty: filterValueNone, FilterIsNotEmpty: filterValueNone,
	}
	uniqueIDConditions = map[FilterCondition]filterValueKind{
		FilterEquals: filterValueNumber, FilterDoesNotEqual: filterValueNumber, FilterGreaterThan: filterValueNumber,
		FilterLessThan: filterValueNumber, FilterGreaterThanOrEqualTo: filterValueNumber, FilterLessThanOrEqualTo: filterValueNumber,
	}
	checkboxConditions = map[FilterCondition]filterValueKind{FilterEquals: filterValueCheckbox, FilterDoesNotEqual: filterValueCheckbox}
	optionConditions   = map[FilterCondition]filterValueKind{
		FilterEquals: filterValueText, FilterDoesNotEqual: filterValueText, FilterIsEmpty: filterValueNone, FilterIsNotEmpty: filterValueNone,
	}
	multiSelectConditions = map[FilterCondition]filterValueKind{
		FilterContains: filterValueText, FilterDoesNotContain: filterValueText, FilterIsEmpty: filterValueNone, FilterIsNotEmpty: filterValueNone,
	}
	referenceConditions = map[FilterCondition]filterValueKind{
		FilterContains: filterValueID, FilterDoesNotContain: filterValueID, FilterIsEmpty: filterValueNone, FilterIsNotEmpty: filterValueNone,
	}
	dateConditions = map[FilterCondition]filterValueKind{
		FilterEquals: filterValueDate, FilterBefore: filterValueDate, FilterAfter: filterValueDate,
		FilterOnOrBefore: filterValueDate, FilterOnOrAfter: filterValueDate,
		FilterIsEmpty: filterValueNone, FilterIsNotEmpty: filterValueNone,
		FilterPastWeek: filterValueRelative, FilterPastMonth: filterValueRelative, FilterPastYear: filterValueRelative,
		FilterNextWeek: filterValueRelative, FilterNextMonth: filterValueRelative, FilterNextYear: filterValueRelative,
		FilterThisWeek: filterValueRelative,
	}
	timestampConditions = map[FilterCondition]filterValueKind{
		FilterEquals: filterValueDate, FilterBefore: filterValueDate, FilterAfter: filterValueDate,
		FilterOnOrBefore: filterValueDate, FilterOnOrAfter: filterValueDate,
		FilterPastWeek: filterValueRelative, FilterPastMonth: filterValueRelative, FilterPastYear: filterValueRelative,
		FilterNextWeek: filterValueRelative, FilterNextMonth: filterValueRelative, FilterNextYear: filterValueRelative,
		FilterThisWeek: filterValueRelative,
	}
	propertyFilterConditions = map[PropertyType]map[FilterCondition]filterValueKind{
		PropertyTypeTitle: textConditions, PropertyTypeRichText: textConditions, PropertyTypeURL: textConditions,
		PropertyTypeEmail: textConditions, PropertyTypePhoneNumber: textConditions,
		PropertyTypeNumber: numberConditions, PropertyTypeUniqueID: uniqueIDConditions, PropertyTypeCheckbox: checkboxConditions,
		PropertyTypeSelect: optionConditions, PropertyTypeStatus: optionConditions, PropertyTypeMultiSelect: multiSelectConditions,
		PropertyTypePeople: referenceConditions, PropertyTypeRelation: referenceConditions,
		PropertyTypeCreatedBy: referenceConditions, PropertyTypeLastEditedBy: referenceConditions,
		PropertyTypeDate: dateConditions, PropertyTypeCreatedTime: dateConditions, PropertyTypeLastEditedTime: dateConditions,
		PropertyTypeFiles: {FilterIsEmpty: filterValueNone, FilterIsNotEmpty: filterValueNone},
	}
)

// filterEncoder counts leaf conditions across one filter tree.
type filterEncoder struct {
	conditionCount int
}

// encodeQueryFilter maps a typed filter onto Notion's filter object.
func encodeQueryFilter(filter QueryFilter) (map[string]any, error) {
	encoder := &filterEncoder{}
	return encoder.encode(filter, 0)
}

func (encoder *filterEncoder) encode(filter QueryFilter, depth int) (map[string]any, error) {
	isCompound := filter.And != nil || filter.Or != nil
	isLeaf := filter.Property != "" || filter.Timestamp != ""
	switch {
	case filter.And != nil && filter.Or != nil, isCompound && isLeaf, filter.Property != "" && filter.Timestamp != "":
		return nil, errors.New("a filter sets exactly one of property, timestamp, and, and or")
	case !isCompound && !isLeaf:
		return nil, errors.New("a filter needs a property, a timestamp, and, or or")
	case isCompound:
		return encoder.encodeCompound(filter, depth)
	case filter.Timestamp != "":
		return encoder.encodeTimestamp(filter)
	default:
		return encoder.encodeProperty(filter)
	}
}

func (encoder *filterEncoder) encodeCompound(filter QueryFilter, depth int) (map[string]any, error) {
	if depth >= maximumFilterNesting {
		return nil, fmt.Errorf("Notion nests and/or filters at most %d levels deep", maximumFilterNesting)
	}
	if filter.Type != "" || filter.Condition != "" || filter.hasValue() {
		return nil, errors.New("an and/or filter sets no type, condition, or value")
	}
	key, children := "and", filter.And
	if filter.Or != nil {
		key, children = "or", filter.Or
	}
	if len(children) == 0 || len(children) > maximumArrayElements {
		return nil, fmt.Errorf("an %s filter holds 1 to %d filters", key, maximumArrayElements)
	}
	encoded := make([]map[string]any, 0, len(children))
	for _, child := range children {
		encodedChild, err := encoder.encode(child, depth+1)
		if err != nil {
			return nil, err
		}
		encoded = append(encoded, encodedChild)
	}
	return map[string]any{key: encoded}, nil
}

func (encoder *filterEncoder) encodeTimestamp(filter QueryFilter) (map[string]any, error) {
	if filter.Timestamp != TimestampCreatedTime && filter.Timestamp != TimestampLastEditedTime {
		return nil, errors.New("a timestamp filter uses created_time or last_edited_time")
	}
	if filter.Type != "" {
		return nil, errors.New("a timestamp filter sets no property type")
	}
	condition, err := encoder.encodeCondition(filter, timestampConditions, string(filter.Timestamp))
	if err != nil {
		return nil, err
	}
	return map[string]any{"timestamp": string(filter.Timestamp), string(filter.Timestamp): condition}, nil
}

func (encoder *filterEncoder) encodeProperty(filter QueryFilter) (map[string]any, error) {
	property := strings.TrimSpace(filter.Property)
	if property != filter.Property || utf8.RuneCountInString(property) > maximumPropertyKeyRunes {
		return nil, fmt.Errorf("a filter property is a name or ID of 1 to %d characters without surrounding spaces", maximumPropertyKeyRunes)
	}
	conditions, isSupported := propertyFilterConditions[filter.Type]
	if !isSupported {
		return nil, fmt.Errorf("property %q: filters on type %q are not supported", property, filter.Type)
	}
	condition, err := encoder.encodeCondition(filter, conditions, string(filter.Type))
	if err != nil {
		return nil, fmt.Errorf("property %q: %w", property, err)
	}
	return map[string]any{"property": property, string(filter.Type): condition}, nil
}

// encodeCondition returns Notion's {condition: value} object after checking the value the condition takes.
func (encoder *filterEncoder) encodeCondition(filter QueryFilter, conditions map[FilterCondition]filterValueKind, subject string) (map[string]any, error) {
	encoder.conditionCount++
	if encoder.conditionCount > MaximumFilterConditions {
		return nil, fmt.Errorf("a filter holds at most %d conditions", MaximumFilterConditions)
	}
	kind, isSupported := conditions[filter.Condition]
	if !isSupported {
		return nil, fmt.Errorf("condition %q is not supported for %s", filter.Condition, subject)
	}
	if expected := kind.fieldName(); filter.valueFieldName() != expected {
		if expected == "" {
			return nil, fmt.Errorf("condition %s takes no value", filter.Condition)
		}
		return nil, fmt.Errorf("condition %s takes exactly the %s value", filter.Condition, expected)
	}
	var value any
	switch kind {
	case filterValueNone:
		value = true
	case filterValueRelative:
		value = map[string]any{}
	case filterValueText:
		text := filter.Text
		if strings.TrimSpace(text) == "" || utf8.RuneCountInString(text) > maximumFilterTextRunes {
			return nil, fmt.Errorf("condition %s needs text of 1 to %d characters", filter.Condition, maximumFilterTextRunes)
		}
		value = text
	case filterValueNumber:
		if math.IsNaN(*filter.Number) || math.IsInf(*filter.Number, 0) {
			return nil, errors.New("a filter number must be finite")
		}
		value = *filter.Number
	case filterValueCheckbox:
		value = *filter.Checkbox
	case filterValueDate:
		date, err := validateDateText(filter.Date, "filter date")
		if err != nil {
			return nil, err
		}
		value = date
	case filterValueID:
		id, err := parseFieldID(filter.ID, "filter id")
		if err != nil {
			return nil, err
		}
		value = id
	}
	return map[string]any{string(filter.Condition): value}, nil
}

// valueFieldName names the single value field set, "" for none, or "several" when more than one is set.
func (filter QueryFilter) valueFieldName() string {
	var names []string
	for _, field := range []struct {
		name  string
		isSet bool
	}{
		{"text", filter.Text != ""}, {"number", filter.Number != nil}, {"checkbox", filter.Checkbox != nil},
		{"date", filter.Date != ""}, {"id", filter.ID != ""},
	} {
		if field.isSet {
			names = append(names, field.name)
		}
	}
	switch len(names) {
	case 0:
		return ""
	case 1:
		return names[0]
	default:
		return "several"
	}
}

func (filter QueryFilter) hasValue() bool { return filter.valueFieldName() != "" }

func (kind filterValueKind) fieldName() string {
	switch kind {
	case filterValueText:
		return "text"
	case filterValueNumber:
		return "number"
	case filterValueCheckbox:
		return "checkbox"
	case filterValueDate:
		return "date"
	case filterValueID:
		return "id"
	default:
		return ""
	}
}

// encodeQuerySorts maps typed sorts onto Notion's sorts array.
func encodeQuerySorts(sorts []QuerySort) ([]map[string]string, error) {
	if len(sorts) > MaximumSorts {
		return nil, fmt.Errorf("a query holds at most %d sorts", MaximumSorts)
	}
	encoded := make([]map[string]string, 0, len(sorts))
	for _, sort := range sorts {
		direction := sort.Direction
		if direction == "" {
			direction = SortAscending
		}
		if direction != SortAscending && direction != SortDescending {
			return nil, errors.New("a sort direction is ascending or descending")
		}
		switch {
		case (sort.Property == "") == (sort.Timestamp == ""):
			return nil, errors.New("a sort sets exactly one of property and timestamp")
		case sort.Timestamp != "":
			if sort.Timestamp != TimestampCreatedTime && sort.Timestamp != TimestampLastEditedTime {
				return nil, errors.New("a timestamp sort uses created_time or last_edited_time")
			}
			encoded = append(encoded, map[string]string{"timestamp": string(sort.Timestamp), "direction": string(direction)})
		default:
			if strings.TrimSpace(sort.Property) != sort.Property || utf8.RuneCountInString(sort.Property) > maximumPropertyKeyRunes {
				return nil, fmt.Errorf("a sort property is a name or ID of 1 to %d characters without surrounding spaces", maximumPropertyKeyRunes)
			}
			encoded = append(encoded, map[string]string{"property": sort.Property, "direction": string(direction)})
		}
	}
	return encoded, nil
}
