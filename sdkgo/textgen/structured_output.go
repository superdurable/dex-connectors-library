// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textgen

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxSchemaDepth = 10
	// These bounds keep exact big.Rat parsing of model-controlled numbers cheap; float64 needs exponents near 308.
	maxJSONNumberLength   = 256
	maxJSONNumberExponent = 400
)

var (
	structuredOutputNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)
	uuidPattern                 = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
)

var portableSchemaKeywords = map[string]bool{
	"type": true, "properties": true, "required": true, "additionalProperties": true, "items": true,
	"enum": true, "const": true, "description": true, "title": true, "minimum": true, "maximum": true,
	"minLength": true, "maxLength": true, "minItems": true, "maxItems": true, "format": true,
}

var portableSchemaFormats = map[string]bool{"date-time": true, "date": true, "time": true, "uuid": true}

// movableSchemaKeywords can be described in text instead of sent as constraints.
var movableSchemaKeywords = map[string]bool{
	"minimum": true, "maximum": true, "minLength": true, "maxLength": true,
	"minItems": true, "maxItems": true, "format": true,
}

// removableSchemaKeywords can be removed without changing the object's shape.
var removableSchemaKeywords = map[string]bool{
	"minimum": true, "maximum": true, "minLength": true, "maxLength": true, "minItems": true,
	"maxItems": true, "format": true, "title": true, "additionalProperties": true,
}

// schemaLocationError names a schema or instance location and a reason, never a value.
type schemaLocationError struct {
	pointer string
	reason  string
}

// Error names the location and reason.
func (err *schemaLocationError) Error() string {
	location := err.pointer
	if location == "" {
		location = "the root"
	}
	return "at " + location + ": " + err.reason
}

// canonicalizeSchema converts application Go values, such as []string, into
// plain JSON values so every later step sees one representation.
func canonicalizeSchema(schema map[string]any) (map[string]any, error) {
	encoded, err := json.Marshal(schema)
	if err != nil {
		return nil, errors.New("schema must be JSON serializable")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var canonical map[string]any
	if err := decoder.Decode(&canonical); err != nil || canonical == nil {
		return nil, errors.New("schema must be a JSON object")
	}
	return canonical, nil
}

// checkPortableSchema enforces the v1 portable subset on a canonical schema.
func checkPortableSchema(schema map[string]any) error {
	primaryType, isNullable, err := readSchemaType(schema, "")
	if err != nil {
		return err
	}
	if primaryType != "object" || isNullable {
		return &schemaLocationError{reason: `the root must have type "object"`}
	}
	return checkSchemaNode(schema, "", 1)
}

func checkSchemaNode(node map[string]any, pointer string, depth int) error {
	if depth > maxSchemaDepth {
		return &schemaLocationError{pointer: pointer, reason: fmt.Sprintf("the schema is nested deeper than %d levels", maxSchemaDepth)}
	}
	for _, keyword := range sortedKeys(node) {
		if !portableSchemaKeywords[keyword] {
			return &schemaLocationError{pointer: pointer, reason: fmt.Sprintf("keyword %q is outside the portable subset", truncateForMessage(keyword))}
		}
	}
	primaryType, _, err := readSchemaType(node, pointer)
	if err != nil {
		return err
	}
	if err := checkKeywordApplicability(node, pointer, primaryType); err != nil {
		return err
	}
	if err := checkAnnotationsAndValues(node, pointer); err != nil {
		return err
	}
	switch primaryType {
	case "object":
		return checkObjectSchema(node, pointer, depth)
	case "array":
		if err := checkCountBounds(node, pointer, "minItems", "maxItems"); err != nil {
			return err
		}
		items, isObject := node["items"].(map[string]any)
		if !isObject {
			return &schemaLocationError{pointer: pointer, reason: `an array requires an "items" schema object`}
		}
		return checkSchemaNode(items, pointer+"/items", depth+1)
	case "string":
		if format, hasFormat := node["format"]; hasFormat {
			if text, isString := format.(string); !isString || !portableSchemaFormats[text] {
				return &schemaLocationError{pointer: pointer, reason: `"format" must be date-time, date, time, or uuid`}
			}
		}
		return checkCountBounds(node, pointer, "minLength", "maxLength")
	case "number", "integer":
		return checkNumberBounds(node, pointer)
	}
	return nil
}

func checkObjectSchema(node map[string]any, pointer string, depth int) error {
	properties, isObject := node["properties"].(map[string]any)
	if !isObject {
		return &schemaLocationError{pointer: pointer, reason: `an object requires a "properties" object`}
	}
	if additional, isBool := node["additionalProperties"].(bool); !isBool || additional {
		return &schemaLocationError{pointer: pointer, reason: `an object must set "additionalProperties" to false`}
	}
	required, isArray := node["required"].([]any)
	if !isArray {
		return &schemaLocationError{pointer: pointer, reason: `an object must list every property in "required"`}
	}
	listed := make(map[string]bool, len(required))
	for _, name := range required {
		text, isString := name.(string)
		if !isString || listed[text] {
			return &schemaLocationError{pointer: pointer, reason: `"required" must list property names once each`}
		}
		if _, isDeclared := properties[text]; !isDeclared {
			return &schemaLocationError{pointer: pointer, reason: `"required" lists a property that "properties" does not declare`}
		}
		listed[text] = true
	}
	if len(listed) != len(properties) {
		return &schemaLocationError{pointer: pointer, reason: `an object must list every property in "required"; use type [T, "null"] for an optional value`}
	}
	for _, name := range sortedKeys(properties) {
		property, isSchema := properties[name].(map[string]any)
		propertyPointer := pointer + "/properties/" + escapeJSONPointerSegment(name)
		if !isSchema {
			return &schemaLocationError{pointer: propertyPointer, reason: "a property schema must be an object"}
		}
		if err := checkSchemaNode(property, propertyPointer, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func checkKeywordApplicability(node map[string]any, pointer string, primaryType string) error {
	allowedFor := map[string]string{
		"properties": "object", "required": "object", "additionalProperties": "object",
		"items": "array", "minItems": "array", "maxItems": "array",
		"minLength": "string", "maxLength": "string", "format": "string",
	}
	for _, keyword := range sortedKeys(node) {
		if requiredType, isTyped := allowedFor[keyword]; isTyped && requiredType != primaryType {
			return &schemaLocationError{pointer: pointer, reason: fmt.Sprintf("keyword %q applies only to type %q", keyword, requiredType)}
		}
		if (keyword == "minimum" || keyword == "maximum") && primaryType != "number" && primaryType != "integer" {
			return &schemaLocationError{pointer: pointer, reason: fmt.Sprintf("keyword %q applies only to number and integer types", keyword)}
		}
	}
	return nil
}

func checkAnnotationsAndValues(node map[string]any, pointer string) error {
	for _, keyword := range []string{"description", "title"} {
		if value, found := node[keyword]; found {
			if _, isString := value.(string); !isString {
				return &schemaLocationError{pointer: pointer, reason: fmt.Sprintf("%q must be a string", keyword)}
			}
		}
	}
	if value, found := node["enum"]; found {
		values, isArray := value.([]any)
		if !isArray || len(values) == 0 {
			return &schemaLocationError{pointer: pointer, reason: `"enum" must be a non-empty array`}
		}
		for _, candidate := range values {
			if !isJSONScalar(candidate) {
				return &schemaLocationError{pointer: pointer, reason: `"enum" values must be strings, numbers, booleans, or null`}
			}
			if !isBoundedJSONScalar(candidate) {
				return &schemaLocationError{pointer: pointer, reason: `"enum" has a number that is out of range`}
			}
		}
	}
	if value, found := node["const"]; found {
		if !isJSONScalar(value) {
			return &schemaLocationError{pointer: pointer, reason: `"const" must be a string, number, boolean, or null`}
		}
		if !isBoundedJSONScalar(value) {
			return &schemaLocationError{pointer: pointer, reason: `"const" is a number that is out of range`}
		}
	}
	return nil
}

func checkCountBounds(node map[string]any, pointer string, minimumKeyword string, maximumKeyword string) error {
	minimum, hasMinimum, err := readCountKeyword(node, pointer, minimumKeyword)
	if err != nil {
		return err
	}
	maximum, hasMaximum, err := readCountKeyword(node, pointer, maximumKeyword)
	if err != nil {
		return err
	}
	if hasMinimum && hasMaximum && minimum > maximum {
		return &schemaLocationError{pointer: pointer, reason: fmt.Sprintf("%q exceeds %q", minimumKeyword, maximumKeyword)}
	}
	return nil
}

func readCountKeyword(node map[string]any, pointer string, keyword string) (int64, bool, error) {
	value, found := node[keyword]
	if !found {
		return 0, false, nil
	}
	number, isNumber := value.(json.Number)
	count, err := strconv.ParseInt(string(number), 10, 64)
	if !isNumber || err != nil || count < 0 {
		return 0, false, &schemaLocationError{pointer: pointer, reason: fmt.Sprintf("%q must be a non-negative integer", keyword)}
	}
	return count, true, nil
}

func checkNumberBounds(node map[string]any, pointer string) error {
	var bounds [2]*big.Rat
	for index, keyword := range []string{"minimum", "maximum"} {
		value, found := node[keyword]
		if !found {
			continue
		}
		number, isNumber := value.(json.Number)
		if !isNumber {
			return &schemaLocationError{pointer: pointer, reason: fmt.Sprintf("%q must be a number", keyword)}
		}
		rational, isBounded := parseBoundedJSONNumber(number)
		if !isBounded {
			return &schemaLocationError{pointer: pointer, reason: fmt.Sprintf("%q is a number that is out of range", keyword)}
		}
		bounds[index] = rational
	}
	if bounds[0] != nil && bounds[1] != nil && bounds[0].Cmp(bounds[1]) > 0 {
		return &schemaLocationError{pointer: pointer, reason: `"minimum" exceeds "maximum"`}
	}
	return nil
}

// readSchemaType returns the node's non-null type and whether null is also allowed.
func readSchemaType(node map[string]any, pointer string) (string, bool, error) {
	isKnownType := func(name string) bool {
		switch name {
		case "object", "array", "string", "number", "integer", "boolean", "null":
			return true
		}
		return false
	}
	switch typed := node["type"].(type) {
	case string:
		if isKnownType(typed) {
			return typed, false, nil
		}
	case []any:
		if len(typed) == 2 {
			first, isFirstString := typed[0].(string)
			second, isSecondString := typed[1].(string)
			switch {
			case isFirstString && isSecondString && second == "null" && first != "null" && isKnownType(first):
				return first, true, nil
			case isFirstString && isSecondString && first == "null" && second != "null" && isKnownType(second):
				return second, true, nil
			}
		}
	}
	return "", false, &schemaLocationError{pointer: pointer, reason: `"type" must be one JSON type or a pair of one type and "null"`}
}

// transformSchemaForProvider returns a copy of a checked canonical schema with
// the rules' keywords moved into descriptions or removed.
func transformSchemaForProvider(canonical map[string]any, rules StructuredOutputRules) (map[string]any, error) {
	if len(rules.KeywordsMovedToDescription) == 0 && len(rules.KeywordsRemoved) == 0 {
		return canonical, nil
	}
	providerSchema, err := canonicalizeSchema(canonical)
	if err != nil {
		return nil, err
	}
	moved := sortedUnique(rules.KeywordsMovedToDescription)
	removed := sortedUnique(rules.KeywordsRemoved)
	doTransformSchemaNode(providerSchema, moved, removed)
	return providerSchema, nil
}

func doTransformSchemaNode(node map[string]any, moved []string, removed []string) {
	var constraints []string
	for _, keyword := range moved {
		value, found := node[keyword]
		if !found {
			continue
		}
		encoded, err := json.Marshal(value)
		if err != nil {
			// Canonical schema values are JSON values, so encoding cannot fail.
			continue
		}
		constraints = append(constraints, keyword+": "+string(encoded))
		delete(node, keyword)
	}
	if len(constraints) > 0 {
		described := "(" + strings.Join(constraints, ", ") + ")"
		if description, _ := node["description"].(string); description != "" {
			described = description + " " + described
		}
		node["description"] = described
	}
	for _, keyword := range removed {
		delete(node, keyword)
	}
	if properties, isObject := node["properties"].(map[string]any); isObject {
		for _, property := range properties {
			if propertySchema, isSchema := property.(map[string]any); isSchema {
				doTransformSchemaNode(propertySchema, moved, removed)
			}
		}
	}
	if items, isSchema := node["items"].(map[string]any); isSchema {
		doTransformSchemaNode(items, moved, removed)
	}
}

// jsonObjectInstruction names the schema and contains "JSON", which some
// json_object providers require before they accept the mode.
func jsonObjectInstruction(output StructuredOutput, canonical map[string]any) (string, error) {
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", errors.New("schema must be JSON serializable")
	}
	instruction := "Respond with only one JSON object, without Markdown code fences, that matches the JSON Schema named " +
		strconv.Quote(output.Name)
	if output.Description != "" {
		instruction += " (" + output.Description + ")"
	}
	return instruction + ":\n" + string(encoded), nil
}

// stripJSONFence removes surrounding whitespace and one Markdown code fence
// whose info string is empty or "json".
func stripJSONFence(text string) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) < 6 || !strings.HasPrefix(trimmed, "```") || !strings.HasSuffix(trimmed, "```") {
		return trimmed
	}
	inner := trimmed[3 : len(trimmed)-3]
	newline := strings.IndexByte(inner, '\n')
	if newline < 0 {
		return trimmed
	}
	if info := strings.TrimSpace(inner[:newline]); info != "" && !strings.EqualFold(info, "json") {
		return trimmed
	}
	return strings.TrimSpace(inner[newline+1:])
}

// validateStructuredOutputText checks text against a canonical schema; errors name a JSON pointer, never a value.
func validateStructuredOutputText(text string, schema map[string]any) error {
	decoder := json.NewDecoder(strings.NewReader(text))
	decoder.UseNumber()
	var instance any
	if err := decoder.Decode(&instance); err != nil {
		return &schemaLocationError{reason: "the text is not one JSON value"}
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return &schemaLocationError{reason: "the text has data after the JSON value"}
	}
	return validateInstance(instance, schema, "")
}

func validateInstance(instance any, schema map[string]any, pointer string) error {
	primaryType, isNullable, err := readSchemaType(schema, pointer)
	if err != nil {
		return err
	}
	if instance == nil {
		if !isNullable && primaryType != "null" {
			return &schemaLocationError{pointer: pointer, reason: "expected type " + primaryType + ", found null"}
		}
		return validateEnumAndConst(nil, nil, schema, pointer)
	}
	// Each number is parsed once, after its size is bounded, and reused by every check.
	var number *big.Rat
	if literal, isNumber := instance.(json.Number); isNumber {
		parsed, isBounded := parseBoundedJSONNumber(literal)
		if !isBounded {
			return &schemaLocationError{pointer: pointer, reason: "the number is out of range"}
		}
		number = parsed
	}
	if err := validateInstanceType(instance, number, primaryType, pointer); err != nil {
		return err
	}
	if err := validateEnumAndConst(instance, number, schema, pointer); err != nil {
		return err
	}
	switch primaryType {
	case "object":
		return validateObjectInstance(instance.(map[string]any), schema, pointer)
	case "array":
		values := instance.([]any)
		if err := validateCount(int64(len(values)), schema, pointer, "minItems", "maxItems", "items"); err != nil {
			return err
		}
		items, _ := schema["items"].(map[string]any)
		for index, value := range values {
			if err := validateInstance(value, items, pointer+"/"+strconv.Itoa(index)); err != nil {
				return err
			}
		}
	case "string":
		text := instance.(string)
		if err := validateCount(int64(utf8.RuneCountInString(text)), schema, pointer, "minLength", "maxLength", "characters"); err != nil {
			return err
		}
		if format, hasFormat := schema["format"].(string); hasFormat && !matchesStringFormat(text, format) {
			return &schemaLocationError{pointer: pointer, reason: "the string does not match format " + format}
		}
	case "number", "integer":
		return validateNumberBounds(number, schema, pointer)
	}
	return nil
}

// validateInstanceType checks instance against primaryType; number is the parsed value of a json.Number instance.
func validateInstanceType(instance any, number *big.Rat, primaryType string, pointer string) error {
	isMatch := false
	switch instance.(type) {
	case map[string]any:
		isMatch = primaryType == "object"
	case []any:
		isMatch = primaryType == "array"
	case string:
		isMatch = primaryType == "string"
	case bool:
		isMatch = primaryType == "boolean"
	case json.Number:
		isMatch = primaryType == "number" || (primaryType == "integer" && number.IsInt())
	}
	if !isMatch {
		return &schemaLocationError{pointer: pointer, reason: "expected type " + primaryType}
	}
	return nil
}

// validateEnumAndConst applies enum and const to every instance, including null.
func validateEnumAndConst(instance any, number *big.Rat, schema map[string]any, pointer string) error {
	if values, found := schema["enum"].([]any); found && !containsJSONScalar(values, instance, number) {
		return &schemaLocationError{pointer: pointer, reason: "the value is not one of the enum values"}
	}
	if value, found := schema["const"]; found && !isEqualJSONScalar(value, instance, number) {
		return &schemaLocationError{pointer: pointer, reason: "the value does not equal const"}
	}
	return nil
}

func validateObjectInstance(object map[string]any, schema map[string]any, pointer string) error {
	properties, _ := schema["properties"].(map[string]any)
	for _, name := range sortedKeys(object) {
		if _, isDeclared := properties[name]; !isDeclared {
			return &schemaLocationError{pointer: pointer, reason: "the object has a property the schema does not declare"}
		}
	}
	for _, name := range sortedKeys(properties) {
		propertyPointer := pointer + "/" + escapeJSONPointerSegment(name)
		value, found := object[name]
		if !found {
			return &schemaLocationError{pointer: propertyPointer, reason: "a required property is missing"}
		}
		propertySchema, _ := properties[name].(map[string]any)
		if err := validateInstance(value, propertySchema, propertyPointer); err != nil {
			return err
		}
	}
	return nil
}

func validateCount(count int64, schema map[string]any, pointer string, minimumKeyword string, maximumKeyword string, unit string) error {
	if minimum, found := schema[minimumKeyword].(json.Number); found {
		if bound, err := strconv.ParseInt(string(minimum), 10, 64); err == nil && count < bound {
			return &schemaLocationError{pointer: pointer, reason: fmt.Sprintf("the value has fewer %s than %s", unit, minimumKeyword)}
		}
	}
	if maximum, found := schema[maximumKeyword].(json.Number); found {
		if bound, err := strconv.ParseInt(string(maximum), 10, 64); err == nil && count > bound {
			return &schemaLocationError{pointer: pointer, reason: fmt.Sprintf("the value has more %s than %s", unit, maximumKeyword)}
		}
	}
	return nil
}

func validateNumberBounds(value *big.Rat, schema map[string]any, pointer string) error {
	if minimum, found := schema["minimum"].(json.Number); found {
		if bound, isBound := parseBoundedJSONNumber(minimum); isBound && value.Cmp(bound) < 0 {
			return &schemaLocationError{pointer: pointer, reason: "the value is below minimum"}
		}
	}
	if maximum, found := schema["maximum"].(json.Number); found {
		if bound, isBound := parseBoundedJSONNumber(maximum); isBound && value.Cmp(bound) > 0 {
			return &schemaLocationError{pointer: pointer, reason: "the value is above maximum"}
		}
	}
	return nil
}

// parseBoundedJSONNumber refuses an overlong literal or an oversized exponent before any big.Rat work.
func parseBoundedJSONNumber(number json.Number) (*big.Rat, bool) {
	literal := string(number)
	if literal == "" || len(literal) > maxJSONNumberLength {
		return nil, false
	}
	if index := strings.IndexAny(literal, "eE"); index >= 0 {
		exponent, err := strconv.Atoi(literal[index+1:])
		if err != nil || exponent > maxJSONNumberExponent || exponent < -maxJSONNumberExponent {
			return nil, false
		}
	}
	return new(big.Rat).SetString(literal)
}

func matchesStringFormat(text string, format string) bool {
	switch format {
	case "date-time":
		_, err := time.Parse(time.RFC3339, text)
		return err == nil
	case "date":
		_, err := time.Parse(time.DateOnly, text)
		return err == nil && len(text) == len(time.DateOnly)
	case "time":
		_, err := time.Parse("15:04:05Z07:00", text)
		return err == nil
	case "uuid":
		return uuidPattern.MatchString(text)
	default:
		return false
	}
}

func isJSONScalar(value any) bool {
	switch value.(type) {
	case nil, string, bool, json.Number:
		return true
	default:
		return false
	}
}

// isBoundedJSONScalar reports whether a scalar is not a number, or is a number parseBoundedJSONNumber accepts.
func isBoundedJSONScalar(value any) bool {
	number, isNumber := value.(json.Number)
	if !isNumber {
		return true
	}
	_, isBounded := parseBoundedJSONNumber(number)
	return isBounded
}

func containsJSONScalar(values []any, instance any, instanceNumber *big.Rat) bool {
	for _, candidate := range values {
		if isEqualJSONScalar(candidate, instance, instanceNumber) {
			return true
		}
	}
	return false
}

// isEqualJSONScalar compares a schema scalar with an instance; actualNumber is the parsed value of a json.Number instance.
func isEqualJSONScalar(expected any, actual any, actualNumber *big.Rat) bool {
	switch typedExpected := expected.(type) {
	case nil:
		return actual == nil
	case string:
		typedActual, isString := actual.(string)
		return isString && typedActual == typedExpected
	case bool:
		typedActual, isBool := actual.(bool)
		return isBool && typedActual == typedExpected
	case json.Number:
		if actualNumber == nil {
			return false
		}
		expectedValue, isExpected := parseBoundedJSONNumber(typedExpected)
		return isExpected && expectedValue.Cmp(actualNumber) == 0
	default:
		return false
	}
}

func escapeJSONPointerSegment(segment string) string {
	return strings.ReplaceAll(strings.ReplaceAll(segment, "~", "~0"), "/", "~1")
}

func truncateForMessage(value string) string {
	if len(value) <= 64 {
		return value
	}
	return value[:64] + "..."
}

func sortedKeys[V any](values map[string]V) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

func sortedUnique(values []string) []string {
	seen := make(map[string]bool, len(values))
	unique := make([]string, 0, len(values))
	for _, value := range values {
		if !seen[value] {
			seen[value] = true
			unique = append(unique, value)
		}
	}
	sort.Strings(unique)
	return unique
}
