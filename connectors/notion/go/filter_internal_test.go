// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package notion

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTypedFiltersMapOntoNotionsFilterObject(t *testing.T) {
	minimumScore, isChecked := 3.0, true
	filter := AllOf(
		QueryFilter{Property: "Submission ID", Type: PropertyTypeRichText, Condition: FilterEquals, Text: "sub-42"},
		QueryFilter{Property: "Score", Type: PropertyTypeNumber, Condition: FilterGreaterThanOrEqualTo, Number: &minimumScore},
		QueryFilter{Property: "Reviewed", Type: PropertyTypeCheckbox, Condition: FilterEquals, Checkbox: &isChecked},
		AnyOf(
			QueryFilter{Property: "Status", Type: PropertyTypeStatus, Condition: FilterEquals, Text: "New"},
			QueryFilter{Property: "Email", Type: PropertyTypeEmail, Condition: FilterIsEmpty},
			QueryFilter{Property: "Owner", Type: PropertyTypePeople, Condition: FilterContains, ID: "0b3c9a2e5b7d4e8a9c0123456789abcd"},
		),
		QueryFilter{Timestamp: TimestampCreatedTime, Condition: FilterOnOrAfter, Date: "2026-09-01"},
		QueryFilter{Property: "Due", Type: PropertyTypeDate, Condition: FilterNextWeek},
	)
	encoded, err := encodeQueryFilter(filter)
	require.NoError(t, err)
	contents, err := json.Marshal(encoded)
	require.NoError(t, err)
	require.JSONEq(t, `{"and":[
		{"property":"Submission ID","rich_text":{"equals":"sub-42"}},
		{"property":"Score","number":{"greater_than_or_equal_to":3}},
		{"property":"Reviewed","checkbox":{"equals":true}},
		{"or":[
			{"property":"Status","status":{"equals":"New"}},
			{"property":"Email","email":{"is_empty":true}},
			{"property":"Owner","people":{"contains":"0b3c9a2e-5b7d-4e8a-9c01-23456789abcd"}}
		]},
		{"timestamp":"created_time","created_time":{"on_or_after":"2026-09-01"}},
		{"property":"Due","date":{"next_week":{}}}
	]}`, string(contents))
}

func TestUnsupportedFiltersAreRejectedBeforeAnyRequest(t *testing.T) {
	number := 1.0
	deeplyNested := AllOf(AnyOf(AllOf(QueryFilter{Property: "Name", Type: PropertyTypeTitle, Condition: FilterIsEmpty})))
	for name, filter := range map[string]QueryFilter{
		"an empty filter":                         {},
		"both a property and a compound":          {Property: "Name", Type: PropertyTypeTitle, Condition: FilterIsEmpty, And: []QueryFilter{{}}},
		"both and and or":                         {And: []QueryFilter{}, Or: []QueryFilter{}},
		"an empty and":                            {And: []QueryFilter{}},
		"three levels of compound filters":        deeplyNested,
		"a condition the type lacks":              {Property: "Score", Type: PropertyTypeNumber, Condition: FilterContains, Number: &number},
		"a value of the wrong kind":               {Property: "Score", Type: PropertyTypeNumber, Condition: FilterEquals, Text: "1"},
		"a value on is_empty":                     {Property: "Name", Type: PropertyTypeTitle, Condition: FilterIsEmpty, Text: "x"},
		"two values":                              {Property: "Name", Type: PropertyTypeTitle, Condition: FilterEquals, Text: "x", Number: &number},
		"a missing value":                         {Property: "Name", Type: PropertyTypeTitle, Condition: FilterEquals},
		"a formula filter":                        {Property: "Total", Type: PropertyTypeFormula, Condition: FilterEquals, Number: &number},
		"a timestamp with a property type":        {Timestamp: TimestampCreatedTime, Type: PropertyTypeDate, Condition: FilterPastWeek},
		"an unknown timestamp":                    {Timestamp: "edited", Condition: FilterPastWeek},
		"an invalid date":                         {Property: "Due", Type: PropertyTypeDate, Condition: FilterBefore, Date: "yesterday"},
		"a person that is not an ID":              {Property: "Owner", Type: PropertyTypePeople, Condition: FilterContains, ID: "me"},
		"a property name with surrounding spaces": {Property: "Name ", Type: PropertyTypeTitle, Condition: FilterIsEmpty},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := encodeQueryFilter(filter)
			require.Error(t, err)
		})
	}
}

func TestFiltersAreBoundedToOneHundredConditions(t *testing.T) {
	conditions := make([]QueryFilter, 0, MaximumFilterConditions)
	for range MaximumFilterConditions {
		conditions = append(conditions, QueryFilter{Property: "Name", Type: PropertyTypeTitle, Condition: FilterIsNotEmpty})
	}
	_, err := encodeQueryFilter(AllOf(conditions...))
	require.NoError(t, err)
	_, err = encodeQueryFilter(AllOf(AllOf(conditions...), QueryFilter{Property: "Name", Type: PropertyTypeTitle, Condition: FilterIsEmpty}))
	require.ErrorContains(t, err, "at most 100 conditions")
}

func TestSortsMapOntoNotionsSortsArray(t *testing.T) {
	sorts, err := encodeQuerySorts([]QuerySort{
		{Property: "Score", Direction: SortDescending}, {Timestamp: TimestampCreatedTime}, {Property: "Name"},
	})
	require.NoError(t, err)
	require.Equal(t, []map[string]string{
		{"property": "Score", "direction": "descending"},
		{"timestamp": "created_time", "direction": "ascending"},
		{"property": "Name", "direction": "ascending"},
	}, sorts)
	for _, invalid := range [][]QuerySort{
		{{}},
		{{Property: "Score", Timestamp: TimestampCreatedTime}},
		{{Property: "Score", Direction: "up"}},
		{{Timestamp: "edited"}},
		make([]QuerySort, MaximumSorts+1),
	} {
		_, err := encodeQuerySorts(invalid)
		require.Error(t, err)
	}
}
