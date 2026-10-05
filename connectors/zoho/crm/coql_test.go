// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package crm_test

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex-connectors-library/connectors/zoho/crm"
)

func condition(field string, operator crm.ConditionOperator, value crm.COQLValue) crm.RecordCondition {
	return crm.RecordCondition{Field: field, Operator: operator, Value: &value}
}

func TestBuildFindRecordsQueryWritesTypedValuesAndNestsConditionsInPairs(t *testing.T) {
	query, err := crm.BuildFindRecordsQuery(crm.FindRecordsInput{
		Module: crm.ModuleDeals, Fields: []string{"Deal_Name", "Stage", "id", "Account_Name.Account_Name"},
		Conditions: []crm.RecordCondition{
			condition("Contact_Name", crm.ConditionEquals, crm.COQLRecordID(testContactID)),
			condition("Stage", crm.ConditionNotIn, crm.COQLTextList([]string{"Closed Won", "Closed Lost"})),
			condition("Amount", crm.ConditionGreaterOrEqual, crm.COQLNumber("50000.50")),
			condition("Closing_Date", crm.ConditionLessThan, crm.COQLDate(time.Date(2026, 3, 31, 23, 0, 0, 0, time.UTC))),
			condition("Modified_Time", crm.ConditionGreaterThan, crm.COQLDateTime(time.Date(2026, 1, 28, 18, 30, 0, 0, time.FixedZone("IST", 19800)))),
			condition("Email_Opt_Out", crm.ConditionEquals, crm.COQLBoolean(false)),
			{Field: "Next_Step", Operator: crm.ConditionIsNull},
		},
		Sort: &crm.RecordSort{Field: "Modified_Time", IsDescending: true}, Limit: 5, Offset: 10,
	})
	require.NoError(t, err)
	require.Equal(t, "select Deal_Name, Stage, Account_Name.Account_Name from Deals where "+
		"((((((Contact_Name = '"+testContactID+"' and Stage not in ('Closed Won', 'Closed Lost')) and Amount >= 50000.50)"+
		" and Closing_Date < '2026-03-31') and Modified_Time > '2026-01-28T13:00:00+00:00') and Email_Opt_Out = false) and Next_Step is null)"+
		" order by Modified_Time desc, id asc limit 10, 5", query)

	single, err := crm.BuildFindRecordsQuery(crm.FindRecordsInput{
		Module: crm.ModuleContacts, Fields: []string{"Email"},
		Conditions: []crm.RecordCondition{condition("Email", crm.ConditionEquals, crm.COQLText("jane@acme.example.com"))},
	})
	require.NoError(t, err)
	require.Equal(t, "select Email from Contacts where Email = 'jane@acme.example.com' order by id asc limit 0, 10", single)
}

func TestBuildFindRecordsQueryRejectsValuesThatCouldLeaveTheirLiteralWithoutRepeatingThem(t *testing.T) {
	valid := crm.FindRecordsInput{
		Module: crm.ModuleAccounts, Fields: []string{"Account_Name"},
		Conditions: []crm.RecordCondition{condition("Account_Name", crm.ConditionEquals, crm.COQLText("Acme Corp"))},
	}
	for name, change := range map[string]func(*crm.FindRecordsInput){
		"apostrophe":            func(input *crm.FindRecordsInput) { input.Conditions[0].Value.Text = "Macy's SENTINEL" },
		"backslash":             func(input *crm.FindRecordsInput) { input.Conditions[0].Value.Text = `Acme\ SENTINEL` },
		"line break":            func(input *crm.FindRecordsInput) { input.Conditions[0].Value.Text = "Acme\nSENTINEL" },
		"empty text":            func(input *crm.FindRecordsInput) { input.Conditions[0].Value.Text = "" },
		"field injection":       func(input *crm.FindRecordsInput) { input.Conditions[0].Field = "Account_Name = 'x' or id" },
		"module injection":      func(input *crm.FindRecordsInput) { input.Module = "Accounts where" },
		"three joins":           func(input *crm.FindRecordsInput) { input.Fields = []string{"A.B.C.D"} },
		"no fields":             func(input *crm.FindRecordsInput) { input.Fields = nil },
		"repeated field":        func(input *crm.FindRecordsInput) { input.Fields = []string{"Email", "Email"} },
		"no conditions":         func(input *crm.FindRecordsInput) { input.Conditions = nil },
		"list for equals":       func(input *crm.FindRecordsInput) { *input.Conditions[0].Value = crm.COQLTextList([]string{"a"}) },
		"scalar for in":         func(input *crm.FindRecordsInput) { input.Conditions[0].Operator = crm.ConditionIn },
		"text for ordering":     func(input *crm.FindRecordsInput) { input.Conditions[0].Operator = crm.ConditionGreaterThan },
		"value for is null":     func(input *crm.FindRecordsInput) { input.Conditions[0].Operator = crm.ConditionIsNull },
		"unknown operator":      func(input *crm.FindRecordsInput) { input.Conditions[0].Operator = "like" },
		"exponent number":       func(input *crm.FindRecordsInput) { *input.Conditions[0].Value = crm.COQLNumber("1e9") },
		"non-numeric record ID": func(input *crm.FindRecordsInput) { *input.Conditions[0].Value = crm.COQLRecordID("acct_001") },
		"limit above 200":       func(input *crm.FindRecordsInput) { input.Limit = 201 },
		"negative offset":       func(input *crm.FindRecordsInput) { input.Offset = -1 },
		"offset past depth":     func(input *crm.FindRecordsInput) { input.Offset = 99_999 },
		"sort injection":        func(input *crm.FindRecordsInput) { input.Sort = &crm.RecordSort{Field: "id; drop"} },
	} {
		t.Run(name, func(t *testing.T) {
			input := valid
			value := *valid.Conditions[0].Value
			input.Conditions = []crm.RecordCondition{{Field: valid.Conditions[0].Field, Operator: valid.Conditions[0].Operator, Value: &value}}
			change(&input)
			_, err := crm.BuildFindRecordsQuery(input)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "SENTINEL")
		})
	}
	tooMany := make([]string, crm.MaxConditionListValues+1)
	for index := range tooMany {
		tooMany[index] = "Stage"
	}
	input := valid
	input.Conditions = []crm.RecordCondition{condition("Stage", crm.ConditionIn, crm.COQLTextList(tooMany))}
	_, err := crm.BuildFindRecordsQuery(input)
	require.ErrorContains(t, err, "1 to 100 entries")
}

func TestBuildListModifiedRecordsQueryStartsAtOrAfterTheInstantAndContinuesAfterTheCursor(t *testing.T) {
	since := time.Date(2026, 1, 28, 18, 30, 0, 999, time.FixedZone("IST", 19800))
	query, err := crm.BuildListModifiedRecordsQuery(crm.ListModifiedRecordsInput{Module: crm.ModuleDeals, Fields: []string{"Deal_Name", "Stage"}, ModifiedSince: since})
	require.NoError(t, err)
	require.Equal(t, "select Deal_Name, Stage, Modified_Time from Deals where Modified_Time >= '2026-01-28T13:00:00+00:00'"+
		" order by Modified_Time asc, id asc limit 0, 100", query)

	query, err = crm.BuildListModifiedRecordsQuery(crm.ListModifiedRecordsInput{
		Module: crm.ModuleDeals, Fields: []string{"Modified_Time", "Stage"}, Cursor: "2026-01-28T13:00:05Z/" + testDealID, Limit: 2,
	})
	require.NoError(t, err)
	require.Equal(t, "select Modified_Time, Stage from Deals where (Modified_Time > '2026-01-28T13:00:05+00:00' or "+
		"(Modified_Time = '2026-01-28T13:00:05+00:00' and id > "+testDealID+")) order by Modified_Time asc, id asc limit 0, 2", query)

	for name, input := range map[string]crm.ListModifiedRecordsInput{
		"neither start":   {Module: crm.ModuleDeals, Fields: []string{"Stage"}},
		"both starts":     {Module: crm.ModuleDeals, Fields: []string{"Stage"}, ModifiedSince: since, Cursor: "2026-01-28T13:00:05Z/0"},
		"foreign cursor":  {Module: crm.ModuleDeals, Fields: []string{"Stage"}, Cursor: "c8582xx9e7c7"},
		"cursor with sql": {Module: crm.ModuleDeals, Fields: []string{"Stage"}, Cursor: "2026-01-28T13:00:05Z/1 or 1=1"},
		"no fields":       {Module: crm.ModuleDeals, ModifiedSince: since},
		"too many fields": {Module: crm.ModuleDeals, Fields: strings.Split(strings.Repeat("F,", 49)+"F", ","), ModifiedSince: since},
	} {
		_, err := crm.BuildListModifiedRecordsQuery(input)
		require.Error(t, err, name)
	}
}
