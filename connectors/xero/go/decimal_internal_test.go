// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/superdurable/dex/sdk-go/dex"
)

func TestDecimalPredicatesNeverUseFloatingPoint(t *testing.T) {
	for value, isZero := range map[Decimal]bool{"0": true, "0.00": true, "-0.000": true, "0.01": false, "100": false, "10.00": false, "": false, "abc": false} {
		require.Equal(t, isZero, value.IsZero(), value)
	}
	for value, isNegative := range map[Decimal]bool{"-0.01": true, "-12": true, "-0.00": false, "5": false, "--1": false} {
		require.Equal(t, isNegative, value.IsNegative(), value)
	}
	require.True(t, isAtMostOneHundred("100.000"))
	require.True(t, isAtMostOneHundred("99.9999"))
	require.False(t, isAtMostOneHundred("100.0001"))
	require.False(t, isAtMostOneHundred("250"))
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
	require.Equal(t, json.Number("49.9500"), jsonNumberLiteral("49.9500"))
}

func TestWireDecimalsKeepXerosExactText(t *testing.T) {
	var decoded struct {
		Number   wireDecimal `json:"Number"`
		String   wireDecimal `json:"String"`
		Null     wireDecimal `json:"Null"`
		Blank    wireDecimal `json:"Blank"`
		Flag     wireBool    `json:"Flag"`
		Unquoted wireBool    `json:"Unquoted"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"Number":1800.0000,"String":"0.10","Null":null,"Blank":"","Flag":"true","Unquoted":true}`), &decoded))
	require.Equal(t, wireDecimal{value: "1800.0000", isPresent: true}, decoded.Number)
	require.Equal(t, wireDecimal{value: "0.10", isPresent: true}, decoded.String)
	require.False(t, decoded.Null.isPresent)
	require.False(t, decoded.Blank.isPresent)
	require.True(t, bool(decoded.Flag))
	require.True(t, bool(decoded.Unquoted))
	for _, contents := range []string{`{"Number":1e3}`, `{"Number":"1,000.00"}`, `{"Number":true}`, `{"Number":0123}`} {
		require.Error(t, json.Unmarshal([]byte(contents), &decoded), contents)
	}
}

func TestXeroDatesAreReadFromDateStringOrDotNetMilliseconds(t *testing.T) {
	date, err := calendarDateFromWire("2026-10-01T00:00:00", "/Date(1518685950940+0000)/")
	require.NoError(t, err)
	require.Equal(t, "2026-10-01", date, "DateString wins")
	date, err = calendarDateFromWire("", "/Date(1518685950940+0000)/")
	require.NoError(t, err)
	require.Equal(t, "2018-02-15", date)
	date, err = calendarDateFromWire("", "")
	require.NoError(t, err)
	require.Empty(t, date)
	_, err = calendarDateFromWire("15/10/2026", "")
	require.Error(t, err)
	instant, err := timestampFromWire("/Date(1439434356790)/")
	require.NoError(t, err)
	require.Equal(t, time.Date(2015, 8, 13, 2, 52, 36, 790000000, time.UTC), instant)
	_, err = timestampFromWire("2015-08-13")
	require.Error(t, err)
}

// internalDexContext is a minimal dex.Context for running one operation outside a Worker.
type internalDexContext struct{ context.Context }

func newInternalDexContext() *internalDexContext {
	return &internalDexContext{Context: context.Background()}
}

func (*internalDexContext) FlowID() string                          { return "xero-flow" }
func (*internalDexContext) RunID() string                           { return "run" }
func (*internalDexContext) FlowStartedAt() time.Time                { return time.Unix(1, 0) }
func (*internalDexContext) StepExecutionID() string                 { return "step" }
func (*internalDexContext) FromStepExecutionID() string             { return "" }
func (*internalDexContext) RecoveryError() *dex.RecoveryErrorInfo   { return nil }
func (*internalDexContext) FirstAttemptAt() time.Time               { return time.Unix(1, 0) }
func (*internalDexContext) Attempt() int32                          { return 1 }
func (*internalDexContext) HasTimerFired() bool                     { return false }
func (*internalDexContext) HasTimerFiredByIndex(int) bool           { return false }
func (*internalDexContext) WaitForMethodFailed() bool               { return false }
func (*internalDexContext) SetStepExecutionLocal(string, any) error { return nil }
func (*internalDexContext) GetStepExecutionLocal(string, any) (bool, error) {
	return false, nil
}
func (*internalDexContext) RecordEvent(string, any) error           { return nil }
func (*internalDexContext) RecordHeartbeat(any) error               { return nil }
func (*internalDexContext) GetLastHeartbeatValue(any) (bool, error) { return false, nil }

var _ dex.Context = (*internalDexContext)(nil)
