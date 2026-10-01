// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package xero

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// Decimal is an exact base-10 number carried as text, such as "1800.00", "0.5", or "-12.25".
// The connector never converts a Decimal to binary floating point: an input is validated and
// sent to Xero as the same JSON number literal, and a value Xero returns keeps Xero's exact
// digits, including trailing zeros. Compare values with IsZero or IsNegative, or with an
// arbitrary-precision decimal library, never by parsing them as float64.
type Decimal string

// decimalPattern is a plain decimal literal without exponent, sign padding, or leading zeros.
var decimalPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,10})?$`)

var errInvalidDecimal = errors.New("value is not an exact decimal number")

// IsZero reports whether the value is a valid decimal whose every digit is zero, such as "0.00".
func (value Decimal) IsZero() bool {
	if !decimalPattern.MatchString(string(value)) {
		return false
	}
	return strings.Trim(string(value), "-0.") == ""
}

// IsNegative reports whether the value is a valid decimal below zero.
func (value Decimal) IsNegative() bool {
	return decimalPattern.MatchString(string(value)) && strings.HasPrefix(string(value), "-") && !value.IsZero()
}

// decimalRules bounds one input amount before it is sent to Xero.
type decimalRules struct {
	maximumFractionDigits int
	maximumIntegerDigits  int
	canBeNegative         bool
	canBeZero             bool
}

// validateInputDecimal checks an application amount; the error names the field, never the value.
func validateInputDecimal(fieldName string, value Decimal, rules decimalRules) error {
	text := string(value)
	if !decimalPattern.MatchString(text) {
		return fmt.Errorf("%s must be an exact decimal number such as 1800.00, without an exponent or leading zeros", fieldName)
	}
	integerPart, fractionPart, _ := strings.Cut(strings.TrimPrefix(text, "-"), ".")
	switch {
	case len(fractionPart) > rules.maximumFractionDigits:
		return fmt.Errorf("%s can have at most %d decimal places", fieldName, rules.maximumFractionDigits)
	case len(integerPart) > rules.maximumIntegerDigits:
		return fmt.Errorf("%s can have at most %d digits before the decimal point", fieldName, rules.maximumIntegerDigits)
	case value.IsNegative() && !rules.canBeNegative:
		return fmt.Errorf("%s cannot be negative", fieldName)
	case value.IsZero() && !rules.canBeZero:
		return fmt.Errorf("%s must be greater than zero", fieldName)
	}
	return nil
}

// jsonNumberLiteral returns the validated decimal as the JSON number literal Xero receives.
func jsonNumberLiteral(value Decimal) json.Number {
	return json.Number(value)
}

// wireDecimal decodes a Xero amount sent as a JSON number or a numeric string, keeping its exact text.
type wireDecimal struct {
	value     Decimal
	isPresent bool
}

// UnmarshalJSON accepts a plain decimal number or string and null; anything else is a protocol error.
func (decimal *wireDecimal) UnmarshalJSON(contents []byte) error {
	trimmed := bytes.TrimSpace(contents)
	if bytes.Equal(trimmed, []byte("null")) {
		*decimal = wireDecimal{}
		return nil
	}
	text := string(trimmed)
	if len(trimmed) > 0 && trimmed[0] == '"' {
		if err := json.Unmarshal(trimmed, &text); err != nil {
			return errInvalidDecimal
		}
		text = strings.TrimSpace(text)
		if text == "" {
			*decimal = wireDecimal{}
			return nil
		}
	}
	if !decimalPattern.MatchString(text) {
		return errInvalidDecimal
	}
	*decimal = wireDecimal{value: Decimal(text), isPresent: true}
	return nil
}

// wireBool decodes a Xero flag sent as a JSON boolean or as the strings "true" and "false".
type wireBool bool

// UnmarshalJSON accepts true, false, "true", "false", and null.
func (flag *wireBool) UnmarshalJSON(contents []byte) error {
	switch strings.ToLower(strings.Trim(string(bytes.TrimSpace(contents)), `"`)) {
	case "true":
		*flag = true
	case "false", "null", "":
		*flag = false
	default:
		return errors.New("value is not a boolean")
	}
	return nil
}
