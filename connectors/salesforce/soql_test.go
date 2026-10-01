// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package salesforce

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestRenderSOQLRendersEveryValueKindAsALiteral(t *testing.T) {
	closeDate := time.Date(2026, time.February, 15, 23, 30, 0, 0, time.FixedZone("PST", -8*3600))
	rendered, err := renderSOQL(
		"SELECT Id FROM :object WHERE :field = :name AND Id IN :ids AND Amount >= :amount AND IsClosed = :closed "+
			"AND NextStep = :nextStep AND CloseDate <= :closeDate AND LastModifiedDate > :modified AND Name LIKE :contains "+
			"AND Name LIKE :prefix AND CreatedDate = LAST_N_DAYS:30 AND Description != 'keep :this \\' literal'",
		map[string]SOQLValue{
			"object": SOQLIdentifier("Opportunity"), "field": SOQLIdentifier("Account.Name"),
			"name": SOQLString(`O'Brien "Big" \ Deal` + "\n"), "ids": SOQLStringList([]string{"006A", "006B"}),
			"amount": SOQLNumber("50000.25"), "closed": SOQLBoolean(false), "nextStep": SOQLNull(),
			"closeDate": SOQLDate(closeDate), "modified": SOQLDateTime(closeDate),
			"contains": SOQLLikeContains("50%_off"), "prefix": SOQLLikeStartsWith("Meridian"),
		},
	)
	require.NoError(t, err)
	require.Equal(t, `SELECT Id FROM Opportunity WHERE Account.Name = 'O\'Brien \"Big\" \\ Deal\n' AND Id IN ('006A', '006B') `+
		`AND Amount >= 50000.25 AND IsClosed = FALSE AND NextStep = NULL AND CloseDate <= 2026-02-15 `+
		`AND LastModifiedDate > 2026-02-16T07:30:00Z AND Name LIKE '%50\%\_off%' AND Name LIKE 'Meridian%' `+
		`AND CreatedDate = LAST_N_DAYS:30 AND Description != 'keep :this \' literal'`, rendered)
}

func TestRenderSOQLKeepsBoundTextInsideItsLiteral(t *testing.T) {
	rendered, err := renderSOQL("SELECT Id FROM Contact WHERE Email = :email", map[string]SOQLValue{
		"email": SOQLString(`x' OR Email != 'y`),
	})
	require.NoError(t, err)
	require.Equal(t, `SELECT Id FROM Contact WHERE Email = 'x\' OR Email != \'y'`, rendered)
}

func TestRenderSOQLRejectsInvalidStatementsAndBindingsWithoutRepeatingValues(t *testing.T) {
	for name, test := range map[string]struct {
		statement string
		bindings  map[string]SOQLValue
	}{
		"blank":                  {statement: "  "},
		"missing binding":        {statement: "SELECT Id FROM Contact WHERE Email = :email"},
		"unused binding":         {statement: "SELECT Id FROM Contact", bindings: map[string]SOQLValue{"email": SOQLString("SECRET-VALUE")}},
		"unterminated literal":   {statement: "SELECT Id FROM Contact WHERE Name = 'open"},
		"invalid binding name":   {statement: "SELECT Id FROM Contact", bindings: map[string]SOQLValue{"1st": SOQLNull()}},
		"control character":      {statement: "SELECT Id FROM Contact WHERE Name = :name", bindings: map[string]SOQLValue{"name": SOQLString("SECRET-VALUE\x00")}},
		"expression number":      {statement: "SELECT Id FROM Opportunity WHERE Amount > :amount", bindings: map[string]SOQLValue{"amount": SOQLNumber("1 OR Amount < 0")}},
		"exponent number":        {statement: "SELECT Id FROM Opportunity WHERE Amount > :amount", bindings: map[string]SOQLValue{"amount": SOQLNumber("1e9")}},
		"identifier expression":  {statement: "SELECT :field FROM Contact", bindings: map[string]SOQLValue{"field": SOQLIdentifier("Id, (SELECT Id FROM Cases)")}},
		"empty list":             {statement: "SELECT Id FROM Contact WHERE Id IN :ids", bindings: map[string]SOQLValue{"ids": SOQLStringList(nil)}},
		"invalid date":           {statement: "SELECT Id FROM Opportunity WHERE CloseDate = :day", bindings: map[string]SOQLValue{"day": {Kind: SOQLValueKindDate, Text: "2026-13-01"}}},
		"invalid dateTime":       {statement: "SELECT Id FROM Contact WHERE CreatedDate > :at", bindings: map[string]SOQLValue{"at": {Kind: SOQLValueKindDateTime, Text: "yesterday"}}},
		"unknown kind":           {statement: "SELECT Id FROM Contact WHERE Name = :name", bindings: map[string]SOQLValue{"name": {Kind: "raw", Text: "SECRET-VALUE"}}},
		"invalid UTF-8 value":    {statement: "SELECT Id FROM Contact WHERE Name = :name", bindings: map[string]SOQLValue{"name": SOQLString("\xff")}},
		"invalid UTF-8 template": {statement: "SELECT Id FROM Contact WHERE Name = '\xff'"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := renderSOQL(test.statement, test.bindings)
			require.Error(t, err)
			require.NotContains(t, err.Error(), "SECRET-VALUE")
			require.NotContains(t, err.Error(), "OR Amount")
		})
	}
}

func TestSOQLValueSurvivesDurableStepInput(t *testing.T) {
	values := map[string]SOQLValue{
		"ids": SOQLStringList([]string{"006A"}), "closed": SOQLBoolean(true), "at": SOQLDateTime(time.Unix(1_800_000_000, 0)),
	}
	encoded, err := json.Marshal(values)
	require.NoError(t, err)
	var decoded map[string]SOQLValue
	require.NoError(t, json.Unmarshal(encoded, &decoded))
	require.Equal(t, values, decoded)
	rendered, err := renderSOQL("SELECT Id FROM Opportunity WHERE Id IN :ids AND IsClosed = :closed AND CreatedDate < :at", decoded)
	require.NoError(t, err)
	require.Equal(t, "SELECT Id FROM Opportunity WHERE Id IN ('006A') AND IsClosed = TRUE AND CreatedDate < 2027-01-15T08:00:00Z", rendered)
}
