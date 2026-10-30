// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDecimalPredicatesNeverUseFloatingPoint(t *testing.T) {
	for value, isZero := range map[Decimal]bool{"0": true, "0.00": true, "-0.000": true, "0.01": false, "100": false, "": false, "abc": false} {
		require.Equal(t, isZero, value.IsZero(), value)
	}
	for value, isNegative := range map[Decimal]bool{"-0.01": true, "-12": true, "-0.00": false, "5": false, "--1": false} {
		require.Equal(t, isNegative, value.IsNegative(), value)
	}
	for value, isPositive := range map[Decimal]bool{"0.01": true, "75.40": true, "0": false, "-1": false, "1e2": false} {
		require.Equal(t, isPositive, value.IsPositive(), value)
	}
}

func TestInputDecimalsMustBeExactPlainLiterals(t *testing.T) {
	rules := decimalRules{maximumFractionDigits: 2, maximumIntegerDigits: 4}
	for _, value := range []Decimal{"1", "0.5", "1234.56", "0.01"} {
		require.NoError(t, validateInputDecimal("amount", value, rules), value)
	}
	for _, value := range []Decimal{"", "1.", ".5", "01", "1e2", "1E2", "+1", "1,000", "12345", "1.234", " 7", "NaN", "Infinity", "0", "-1"} {
		err := validateInputDecimal("amount", value, rules)
		require.Error(t, err, value)
		if value != "" {
			require.NotContains(t, err.Error(), string(value), "errors name the field, never the value")
		}
	}
	require.Equal(t, json.Number("49.95"), jsonNumberLiteral("49.95"))
}

func TestComputedLineAmountsRoundHalfAwayFromZeroExactly(t *testing.T) {
	for _, test := range []struct{ quantity, unitPrice, amount Decimal }{
		{"2", "49.95", "99.90"}, {"3", "0.33335", "1.00"}, {"1", "0.005", "0.01"}, {"1", "-0.005", "-0.01"},
		{"0.5", "0.01", "0.01"}, {"0", "49.95", "0.00"}, {"1", "-0.004", "0.00"}, {"0.333", "3", "1.00"},
	} {
		amount, err := multiplyRoundedToCents(test.quantity, test.unitPrice)
		require.NoError(t, err)
		require.Equal(t, test.amount, amount, "%s x %s", test.quantity, test.unitPrice)
	}
}

func TestWireDecimalsKeepQuickBooksDigitsAndExpandExponents(t *testing.T) {
	var decoded struct {
		Number   wireDecimal `json:"Number"`
		Integer  wireDecimal `json:"Integer"`
		String   wireDecimal `json:"String"`
		Exponent wireDecimal `json:"Exponent"`
		Small    wireDecimal `json:"Small"`
		Negative wireDecimal `json:"Negative"`
		Null     wireDecimal `json:"Null"`
		Blank    wireDecimal `json:"Blank"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"Number":1800.00,"Integer":0,"String":"0.10","Exponent":1.5E+3,"Small":2.5e-3,
		"Negative":-1.25E1,"Null":null,"Blank":""}`), &decoded))
	require.Equal(t, wireDecimal{value: "1800.00", isPresent: true}, decoded.Number)
	require.Equal(t, wireDecimal{value: "0", isPresent: true}, decoded.Integer)
	require.Equal(t, wireDecimal{value: "0.10", isPresent: true}, decoded.String)
	require.Equal(t, wireDecimal{value: "1500", isPresent: true}, decoded.Exponent)
	require.Equal(t, wireDecimal{value: "0.0025", isPresent: true}, decoded.Small)
	require.Equal(t, wireDecimal{value: "-12.5", isPresent: true}, decoded.Negative)
	require.False(t, decoded.Null.isPresent)
	require.False(t, decoded.Blank.isPresent)
	for _, contents := range []string{`{"Number":"1,000.00"}`, `{"Number":true}`, `{"Number":0123}`, `{"Number":1e99}`} {
		require.Error(t, json.Unmarshal([]byte(contents), &decoded), contents)
	}
}

func TestQueryLiteralsEscapeApostrophesAndRefuseBackslashes(t *testing.T) {
	require.Equal(t, `'Adam\'s Candy Shop'`, quoteQueryLiteral("Adam's Candy Shop"))
	require.Error(t, validateQueryLiteral("displayName", `Adam\'s`, 100))
	require.Error(t, validateQueryLiteral("displayName", "two\nlines", 100))
	require.NoError(t, validateQueryLiteral("displayName", "Café Olé", 100))
}
