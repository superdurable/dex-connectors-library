// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package monday

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"unicode/utf8"
)

// ItemFilterOperator is an items_page rule operator, as monday.com's API spells it.
type ItemFilterOperator string

const (
	// ItemFilterAnyOf matches a value equal to any compare value, such as an exact item name or status index.
	ItemFilterAnyOf ItemFilterOperator = "any_of"
	// ItemFilterNotAnyOf excludes values equal to any compare value.
	ItemFilterNotAnyOf ItemFilterOperator = "not_any_of"
	// ItemFilterIsEmpty matches an empty column and takes no compare value.
	ItemFilterIsEmpty ItemFilterOperator = "is_empty"
	// ItemFilterIsNotEmpty matches a non-empty column and takes no compare value.
	ItemFilterIsNotEmpty ItemFilterOperator = "is_not_empty"
	// ItemFilterGreaterThan compares numbers or dates.
	ItemFilterGreaterThan ItemFilterOperator = "greater_than"
	// ItemFilterGreaterThanOrEquals compares numbers or dates.
	ItemFilterGreaterThanOrEquals ItemFilterOperator = "greater_than_or_equals"
	// ItemFilterLowerThan compares numbers or dates.
	ItemFilterLowerThan ItemFilterOperator = "lower_than"
	// ItemFilterLowerThanOrEqual compares numbers or dates.
	ItemFilterLowerThanOrEqual ItemFilterOperator = "lower_than_or_equal"
	// ItemFilterBetween matches a value inside two compare values, such as two YYYY-MM-DD dates.
	ItemFilterBetween ItemFilterOperator = "between"
	// ItemFilterContainsText matches text containing the one compare value.
	ItemFilterContainsText ItemFilterOperator = "contains_text"
	// ItemFilterNotContainsText excludes text containing the one compare value.
	ItemFilterNotContainsText ItemFilterOperator = "not_contains_text"
	// ItemFilterContainsTerms matches a label containing the one compare value, such as a status label.
	ItemFilterContainsTerms ItemFilterOperator = "contains_terms"
	// ItemFilterStartsWith matches text starting with the one compare value.
	ItemFilterStartsWith ItemFilterOperator = "starts_with"
	// ItemFilterEndsWith matches text ending with the one compare value.
	ItemFilterEndsWith ItemFilterOperator = "ends_with"
	// ItemFilterWithinTheNext matches dates within the next period the compare values name.
	ItemFilterWithinTheNext ItemFilterOperator = "within_the_next"
	// ItemFilterWithinTheLast matches dates within the last period the compare values name.
	ItemFilterWithinTheLast ItemFilterOperator = "within_the_last"
)

// ItemFilterCombination joins a filter's rules.
type ItemFilterCombination string

const (
	// ItemFilterAllRules matches items that satisfy every rule, monday.com's default.
	ItemFilterAllRules ItemFilterCombination = "and"
	// ItemFilterAnyRule matches items that satisfy at least one rule.
	ItemFilterAnyRule ItemFilterCombination = "or"
)

const (
	// MaxItemFilterRules bounds the rules of one listItems filter.
	MaxItemFilterRules = 10
	// MaxItemFilterValues bounds the compare values of one rule.
	MaxItemFilterValues      = 50
	maxFilterValueCharacters = 1000
	// ItemOrderCreationTime orders by item creation instead of a column.
	ItemOrderCreationTime = "__creation_log__"
	// ItemOrderLastUpdated orders by the last item update instead of a column.
	ItemOrderLastUpdated = "__last_updated__"
)

// ItemFilter is a typed items_page filter: rules on column values joined by Combination.
// The zero value matches every active item on the board.
type ItemFilter struct {
	// Rules are at most 10 column conditions.
	Rules []ItemFilterRule `json:"rules,omitempty"`
	// Combination joins the rules; blank means every rule must match.
	Combination ItemFilterCombination `json:"combination,omitempty"`
}

// ItemFilterRule is one condition on a column's value.
//
// Values carry string compare values in monday.com's per-column format, such as
// an exact item name for the name column, ["EXACT", "2026-07-01"] or ["TODAY"]
// for a date column, or "person-48202303" for a people column. Numbers carry
// numeric compare values, such as status label indexes. Set at most one of the
// two: ItemFilterIsEmpty and ItemFilterIsNotEmpty take neither; contains_text,
// not_contains_text, contains_terms, starts_with, and ends_with take exactly one
// string; between takes exactly two; the other operators take 1 to 50. An
// unsupported combination selects defect before any request is sent.
type ItemFilterRule struct {
	// ColumnID is the column to compare, such as status or date4; name compares the item name.
	ColumnID string `json:"columnId"`
	// Operator is the comparison; blank means any_of.
	Operator ItemFilterOperator `json:"operator,omitempty"`
	// Values are string compare values.
	Values []string `json:"values,omitempty"`
	// Numbers are numeric compare values, such as status label indexes.
	Numbers []float64 `json:"numbers,omitempty"`
	// CompareAttribute optionally names the column attribute monday.com compares, such as a people column's attribute.
	CompareAttribute string `json:"compareAttribute,omitempty"`
}

// ItemOrder orders a listing by one column or timestamp.
type ItemOrder struct {
	// ColumnID is the column to order by, or ItemOrderCreationTime or ItemOrderLastUpdated.
	ColumnID string `json:"columnId"`
	// IsDescending orders from the largest or latest value.
	IsDescending bool `json:"descending,omitempty"`
}

var singleTextFilterOperators = map[ItemFilterOperator]bool{
	ItemFilterContainsText: true, ItemFilterNotContainsText: true, ItemFilterContainsTerms: true,
	ItemFilterStartsWith: true, ItemFilterEndsWith: true,
}

var supportedFilterOperators = map[ItemFilterOperator]bool{
	ItemFilterAnyOf: true, ItemFilterNotAnyOf: true, ItemFilterIsEmpty: true, ItemFilterIsNotEmpty: true,
	ItemFilterGreaterThan: true, ItemFilterGreaterThanOrEquals: true, ItemFilterLowerThan: true,
	ItemFilterLowerThanOrEqual: true, ItemFilterBetween: true, ItemFilterContainsText: true,
	ItemFilterNotContainsText: true, ItemFilterContainsTerms: true, ItemFilterStartsWith: true,
	ItemFilterEndsWith: true, ItemFilterWithinTheNext: true, ItemFilterWithinTheLast: true,
}

// isZero reports a filter without rules or combination.
func (filter ItemFilter) isZero() bool { return len(filter.Rules) == 0 && filter.Combination == "" }

// encodeItemsQuery returns monday.com's ItemsQuery input, or nil when neither a filter nor an order is set.
func encodeItemsQuery(filter ItemFilter, order *ItemOrder) (map[string]any, error) {
	if filter.isZero() && order == nil {
		return nil, nil
	}
	query := map[string]any{}
	if len(filter.Rules) > MaxItemFilterRules {
		return nil, fmt.Errorf("filter accepts at most %d rules", MaxItemFilterRules)
	}
	switch filter.Combination {
	case "":
	case ItemFilterAllRules, ItemFilterAnyRule:
		if len(filter.Rules) == 0 {
			return nil, errors.New("filter combination needs at least one rule")
		}
		query["operator"] = string(filter.Combination)
	default:
		return nil, errors.New("filter combination must be and or or")
	}
	if len(filter.Rules) != 0 {
		rules := make([]map[string]any, 0, len(filter.Rules))
		for index, rule := range filter.Rules {
			encoded, err := encodeItemFilterRule(rule)
			if err != nil {
				return nil, fmt.Errorf("filter rule %d: %w", index+1, err)
			}
			rules = append(rules, encoded)
		}
		query["rules"] = rules
	}
	if order != nil {
		if !columnIDPattern.MatchString(order.ColumnID) {
			return nil, errors.New("order columnId must be a monday.com column ID or __creation_log__ or __last_updated__")
		}
		direction := "asc"
		if order.IsDescending {
			direction = "desc"
		}
		query["order_by"] = []map[string]any{{"column_id": order.ColumnID, "direction": direction}}
	}
	return query, nil
}

func encodeItemFilterRule(rule ItemFilterRule) (map[string]any, error) {
	if !columnIDPattern.MatchString(rule.ColumnID) {
		return nil, errors.New("columnId must be a monday.com column ID such as status, date4, or name")
	}
	operator := rule.Operator
	if operator == "" {
		operator = ItemFilterAnyOf
	}
	if !supportedFilterOperators[operator] {
		return nil, errors.New("operator must be a monday.com items_page rule operator such as any_of or contains_text")
	}
	if len(rule.Values) != 0 && len(rule.Numbers) != 0 {
		return nil, errors.New("set at most one of values and numbers")
	}
	for _, value := range rule.Values {
		if err := validateFilterValue(value); err != nil {
			return nil, err
		}
	}
	for _, number := range rule.Numbers {
		if math.IsNaN(number) || math.IsInf(number, 0) {
			return nil, errors.New("numbers must be finite")
		}
	}
	count := len(rule.Values) + len(rule.Numbers)
	encoded := map[string]any{"column_id": rule.ColumnID, "operator": string(operator)}
	switch {
	case operator == ItemFilterIsEmpty || operator == ItemFilterIsNotEmpty:
		if count != 0 {
			return nil, fmt.Errorf("%s takes no compare value", operator)
		}
		encoded["compare_value"] = []string{}
	case singleTextFilterOperators[operator]:
		if len(rule.Values) != 1 {
			return nil, fmt.Errorf("%s takes exactly one string value", operator)
		}
		encoded["compare_value"] = rule.Values[0]
	case operator == ItemFilterBetween && count != 2:
		return nil, errors.New("between takes exactly two compare values")
	case count == 0 || count > MaxItemFilterValues:
		return nil, fmt.Errorf("%s takes 1 to %d compare values", operator, MaxItemFilterValues)
	case len(rule.Numbers) != 0:
		encoded["compare_value"] = rule.Numbers
	default:
		encoded["compare_value"] = rule.Values
	}
	if rule.CompareAttribute != "" {
		if !columnIDPattern.MatchString(rule.CompareAttribute) {
			return nil, errors.New("compareAttribute must be letters, digits, underscores, or hyphens")
		}
		encoded["compare_attribute"] = rule.CompareAttribute
	}
	return encoded, nil
}

func validateFilterValue(value string) error {
	switch {
	case strings.TrimSpace(value) == "":
		return errors.New("compare values cannot be blank")
	case !utf8.ValidString(value):
		return errors.New("compare values must be valid UTF-8")
	case utf8.RuneCountInString(value) > maxFilterValueCharacters:
		return fmt.Errorf("compare values are at most %d characters", maxFilterValueCharacters)
	}
	return nil
}
