// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package quickbooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"regexp"
	"strconv"
	"strings"
)

// Decimal is an exact base-10 number carried as text, such as "1800.00", "0.5", or "-12.25".
// The connector never converts a Decimal to binary floating point: an input is validated and
// sent to QuickBooks as the same JSON number literal, and a value QuickBooks returns keeps its
// exact digits. Compare values with IsZero, IsNegative, or IsPositive, or with an
// arbitrary-precision decimal library, never by parsing them as float64.
type Decimal string

var (
	// decimalPattern is a plain decimal literal without exponent, sign padding, or leading zeros.
	decimalPattern = regexp.MustCompile(`^-?(?:0|[1-9][0-9]{0,17})(?:\.[0-9]{1,10})?$`)
	// wireNumberPattern is a JSON number, which QuickBooks may write with an exponent.
	wireNumberPattern = regexp.MustCompile(`^(-?)(0|[1-9][0-9]{0,17})(?:\.([0-9]{1,18}))?(?:[eE]([+-]?[0-9]{1,2}))?$`)

	errInvalidDecimal = errors.New("value is not an exact decimal number")
)

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

// IsPositive reports whether the value is a valid decimal above zero, such as an open invoice Balance.
func (value Decimal) IsPositive() bool {
	return decimalPattern.MatchString(string(value)) && !value.IsNegative() && !value.IsZero()
}

// decimalRules bounds one input amount before it is sent to QuickBooks.
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

// jsonNumberLiteral returns the validated decimal as the JSON number literal QuickBooks receives.
func jsonNumberLiteral(value Decimal) json.Number {
	return json.Number(value)
}

// multiplyRoundedToCents multiplies exactly and rounds half away from zero to two places; inputs are pre-validated.
func multiplyRoundedToCents(quantity Decimal, unitPrice Decimal) (Decimal, error) {
	quantityValue, isQuantityParsed := new(big.Rat).SetString(string(quantity))
	unitPriceValue, isUnitPriceParsed := new(big.Rat).SetString(string(unitPrice))
	if !isQuantityParsed || !isUnitPriceParsed {
		return "", errInvalidDecimal
	}
	// FloatString rounds the last digit half away from zero, which is exact for a rational.
	product := new(big.Rat).Mul(quantityValue, unitPriceValue).FloatString(2)
	if strings.Trim(product, "-0.") == "" {
		product = "0.00"
	}
	return Decimal(product), nil
}

// wireDecimal decodes a QuickBooks number or numeric string, keeping its digits and expanding any exponent exactly.
type wireDecimal struct {
	value     Decimal
	isPresent bool
}

// UnmarshalJSON accepts a decimal number or string and null; anything else is a protocol error.
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
	plain, err := plainDecimalText(text)
	if err != nil {
		return err
	}
	*decimal = wireDecimal{value: Decimal(plain), isPresent: true}
	return nil
}

// plainDecimalText rewrites a JSON number such as 1.5E+3 as 1500 without rounding.
func plainDecimalText(text string) (string, error) {
	match := wireNumberPattern.FindStringSubmatch(text)
	if match == nil {
		return "", errInvalidDecimal
	}
	sign, integerPart, fractionPart, exponentText := match[1], match[2], match[3], match[4]
	exponent := 0
	if exponentText != "" {
		parsed, err := strconv.Atoi(exponentText)
		if err != nil {
			return "", errInvalidDecimal
		}
		exponent = parsed
	}
	digits := integerPart + fractionPart
	pointPosition := len(integerPart) + exponent
	switch {
	case pointPosition <= 0:
		digits = strings.Repeat("0", 1-pointPosition) + digits
		pointPosition = 1
	case pointPosition > len(digits):
		digits += strings.Repeat("0", pointPosition-len(digits))
	}
	integerDigits := strings.TrimLeft(digits[:pointPosition], "0")
	if integerDigits == "" {
		integerDigits = "0"
	}
	plain := integerDigits
	if fraction := digits[pointPosition:]; fraction != "" {
		plain += "." + fraction
	}
	if strings.Trim(plain, "0.") != "" {
		plain = sign + plain
	}
	if !decimalPattern.MatchString(plain) {
		return "", errInvalidDecimal
	}
	return plain, nil
}
