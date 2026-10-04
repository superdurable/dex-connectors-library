// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package textgen

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func objectSchema(properties map[string]any) map[string]any {
	required := make([]any, 0, len(properties))
	for _, name := range sortedKeys(properties) {
		required = append(required, name)
	}
	return map[string]any{"type": "object", "properties": properties, "required": required, "additionalProperties": false}
}

func mustCanonicalSchema(t *testing.T, schema map[string]any) map[string]any {
	t.Helper()
	canonical, err := canonicalizeSchema(schema)
	require.NoError(t, err)
	return canonical
}

func TestPortableSchemaSubset(t *testing.T) {
	accepted := map[string]map[string]any{
		"every portable keyword": objectSchema(map[string]any{
			"name":    map[string]any{"type": "string", "title": "Name", "description": "d", "minLength": 1, "maxLength": 20},
			"when":    map[string]any{"type": "string", "format": "date-time"},
			"id":      map[string]any{"type": "string", "format": "uuid"},
			"kind":    map[string]any{"type": "string", "enum": []any{"a", "b"}},
			"version": map[string]any{"type": "integer", "const": 1},
			"score":   map[string]any{"type": "number", "minimum": -1.5, "maximum": 1e3},
			"note":    map[string]any{"type": []any{"string", "null"}},
			"tags":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "minItems": 0, "maxItems": 5},
			"nested":  objectSchema(map[string]any{"flag": map[string]any{"type": "boolean"}}),
			"either":  map[string]any{"type": []any{"null", "integer"}, "enum": []any{1, 2, nil}},
		}),
		"empty object": objectSchema(map[string]any{}),
	}
	for name, schema := range accepted {
		t.Run("accepts "+name, func(t *testing.T) {
			require.NoError(t, checkPortableSchema(mustCanonicalSchema(t, schema)))
		})
	}
	withProperty := func(property any) map[string]any { return objectSchema(map[string]any{"value": property}) }
	nested := map[string]any{"type": "string"}
	for depth := 0; depth < 10; depth++ {
		nested = objectSchema(map[string]any{"child": nested})
	}
	rejected := map[string]struct {
		schema map[string]any
		reason string
	}{
		"array root":                {map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, `the root must have type "object"`},
		"nullable root":             {map[string]any{"type": []any{"object", "null"}, "properties": map[string]any{}, "required": []any{}, "additionalProperties": false}, `the root must have type "object"`},
		"open object":               {map[string]any{"type": "object", "properties": map[string]any{}, "required": []any{}}, `"additionalProperties" to false`},
		"optional property":         {map[string]any{"type": "object", "additionalProperties": false, "required": []any{}, "properties": map[string]any{"a": map[string]any{"type": "string"}}}, `list every property in "required"`},
		"required unknown property": {map[string]any{"type": "object", "additionalProperties": false, "required": []any{"b"}, "properties": map[string]any{}}, `does not declare`},
		"duplicate required":        {map[string]any{"type": "object", "additionalProperties": false, "required": []any{"a", "a"}, "properties": map[string]any{"a": map[string]any{"type": "string"}}}, `once each`},
		"reference":                 {withProperty(map[string]any{"$ref": "#/$defs/x"}), `keyword "$ref"`},
		"definitions":               {map[string]any{"type": "object", "additionalProperties": false, "required": []any{}, "properties": map[string]any{}, "$defs": map[string]any{}}, `keyword "$defs"`},
		"any of":                    {withProperty(map[string]any{"anyOf": []any{}}), `keyword "anyOf"`},
		"pattern":                   {withProperty(map[string]any{"type": "string", "pattern": "^a$"}), `keyword "pattern"`},
		"missing type":              {withProperty(map[string]any{"description": "untyped"}), `"type" must be`},
		"two non-null types":        {withProperty(map[string]any{"type": []any{"string", "integer"}}), `"type" must be`},
		"unknown type":              {withProperty(map[string]any{"type": "date"}), `"type" must be`},
		"array without items":       {withProperty(map[string]any{"type": "array"}), `requires an "items"`},
		"unsupported format":        {withProperty(map[string]any{"type": "string", "format": "email"}), `"format" must be`},
		"empty enum":                {withProperty(map[string]any{"type": "string", "enum": []any{}}), `non-empty array`},
		"object enum value":         {withProperty(map[string]any{"type": "string", "enum": []any{map[string]any{}}}), `"enum" values`},
		"length on a number":        {withProperty(map[string]any{"type": "number", "maxLength": 3}), `applies only to type "string"`},
		"minimum on a string":       {withProperty(map[string]any{"type": "string", "minimum": 3}), `applies only to number`},
		"negative count":            {withProperty(map[string]any{"type": "string", "minLength": -1}), `non-negative integer`},
		"fractional count":          {withProperty(map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "maxItems": 1.5}), `non-negative integer`},
		"inverted bounds":           {withProperty(map[string]any{"type": "integer", "minimum": 5, "maximum": 1}), `"minimum" exceeds "maximum"`},
		"huge exponent bound":       {withProperty(map[string]any{"type": "number", "minimum": json.Number("1e999999")}), `"minimum" is a number that is out of range`},
		"huge enum number":          {withProperty(map[string]any{"type": "number", "enum": []any{json.Number("1e401")}}), `"enum" has a number that is out of range`},
		"long const number":         {withProperty(map[string]any{"type": "number", "const": json.Number("1" + strings.Repeat("0", 256))}), `"const" is a number that is out of range`},
		"non-string description":    {withProperty(map[string]any{"type": "string", "description": 3}), `"description" must be a string`},
		"eleven levels":             {nested, "nested deeper than 10 levels"},
	}
	for name, testCase := range rejected {
		t.Run("rejects "+name, func(t *testing.T) {
			err := checkPortableSchema(mustCanonicalSchema(t, testCase.schema))
			require.Error(t, err)
			require.Contains(t, err.Error(), testCase.reason)
		})
	}
}

func TestTransformSchemaForProviderLeavesTheOriginalIntact(t *testing.T) {
	original := mustCanonicalSchema(t, objectSchema(map[string]any{
		"when": map[string]any{"type": "string", "format": "date", "description": "The day.", "title": "When"},
		"list": map[string]any{"type": "array", "items": map[string]any{"type": "integer", "maximum": 9}, "maxItems": 3},
	}))
	transformed, err := transformSchemaForProvider(original, StructuredOutputRules{
		KeywordsMovedToDescription: []string{"format", "maximum", "maxItems", "format"},
		KeywordsRemoved:            []string{"title", "additionalProperties"},
	})
	require.NoError(t, err)
	properties := transformed["properties"].(map[string]any)
	require.Equal(t, map[string]any{"type": "string", "description": `The day. (format: "date")`}, properties["when"])
	require.Equal(t, "(maxItems: 3)", properties["list"].(map[string]any)["description"])
	require.Equal(t, "(maximum: 9)", properties["list"].(map[string]any)["items"].(map[string]any)["description"])
	require.NotContains(t, transformed, "additionalProperties")
	require.Equal(t, "date", original["properties"].(map[string]any)["when"].(map[string]any)["format"])
	require.Equal(t, false, original["additionalProperties"])

	unchanged, err := transformSchemaForProvider(original, StructuredOutputRules{})
	require.NoError(t, err)
	require.Equal(t, original, unchanged)
	require.Error(t, StructuredOutputRules{KeywordsMovedToDescription: []string{"type"}}.Validate())
	require.Error(t, StructuredOutputRules{KeywordsRemoved: []string{"properties"}}.Validate())
	require.Error(t, StructuredOutputRules{Mode: "xml"}.Validate())
}

func TestStripJSONFence(t *testing.T) {
	for input, expected := range map[string]string{
		`{"a":1}`:                     `{"a":1}`,
		"  {\"a\":1}\n":               `{"a":1}`,
		"```json\n{\"a\":1}\n```":     `{"a":1}`,
		"```JSON\r\n{\"a\":1}\r\n```": `{"a":1}`,
		"```\n{\"a\":1}\n```":         `{"a":1}`,
		"```yaml\na: 1\n```":          "```yaml\na: 1\n```",
		"```{\"a\":1}```":             "```{\"a\":1}```",
		"Here: ```json\n{}\n```":      "Here: ```json\n{}\n```",
	} {
		require.Equal(t, expected, stripJSONFence(input), "%q", input)
	}
}

func TestValidateStructuredOutputTextNamesPointersNotValues(t *testing.T) {
	schema := mustCanonicalSchema(t, objectSchema(map[string]any{
		"name":  map[string]any{"type": "string", "minLength": 2, "maxLength": 4},
		"count": map[string]any{"type": "integer", "minimum": 0, "maximum": 3},
		"ratio": map[string]any{"type": "number"},
		"kind":  map[string]any{"type": "string", "enum": []any{"a", "b"}},
		"fixed": map[string]any{"type": "number", "const": 1.0},
		"note":  map[string]any{"type": []any{"string", "null"}},
		"color": map[string]any{"type": []any{"string", "null"}, "enum": []any{"red", "blue"}},
		"shade": map[string]any{"type": []any{"string", "null"}, "enum": []any{"dark", nil}},
		"only":  map[string]any{"type": []any{"string", "null"}, "const": "only"},
		"day":   map[string]any{"type": "string", "format": "date"},
		"at":    map[string]any{"type": "string", "format": "date-time"},
		"clock": map[string]any{"type": "string", "format": "time"},
		"id":    map[string]any{"type": "string", "format": "uuid"},
		"tags":  map[string]any{"type": "array", "items": map[string]any{"type": "boolean"}, "maxItems": 2},
		"a/b~c": objectSchema(map[string]any{"deep": map[string]any{"type": "boolean"}}),
	}))
	valid := map[string]any{
		"name": "héé", "count": 3, "ratio": 0.5, "kind": "b", "fixed": 1, "note": nil,
		"color": "red", "shade": nil, "only": "only", "day": "2026-09-27",
		"at": "2026-09-27T10:00:00Z", "clock": "10:00:00Z", "id": "123e4567-e89b-12d3-a456-426614174000",
		"tags": []any{true}, "a/b~c": map[string]any{"deep": false},
	}
	encode := func(overrides map[string]any, removed ...string) string {
		document := map[string]any{}
		for key, value := range valid {
			document[key] = value
		}
		for key, value := range overrides {
			document[key] = value
		}
		for _, key := range removed {
			delete(document, key)
		}
		encoded, err := json.Marshal(document)
		require.NoError(t, err)
		return string(encoded)
	}
	require.NoError(t, validateStructuredOutputText(encode(nil), schema))
	require.NoError(t, validateStructuredOutputText(encode(map[string]any{"count": 2.0}), schema), "2.0 is an integer")
	require.NoError(t, validateStructuredOutputText(encode(map[string]any{"count": json.Number("3e0"), "ratio": json.Number("-1e-400")}), schema),
		"exponents up to 400 are exact")
	failures := map[string]struct {
		text    string
		pointer string
	}{
		"not JSON":                   {"secret-value-canary", "the root"},
		"trailing data":              {encode(nil) + ` {"secret-value-canary":1}`, "the root"},
		"array root":                 {`["secret-value-canary"]`, "the root"},
		"missing property":           {encode(nil, "count"), "/count"},
		"extra property":             {encode(map[string]any{"secret-value-canary": 1}), "the root"},
		"wrong type":                 {encode(map[string]any{"name": 12}), "/name"},
		"fractional integer":         {encode(map[string]any{"count": 1.5}), "/count"},
		"too short":                  {encode(map[string]any{"name": "s"}), "/name"},
		"too long":                   {encode(map[string]any{"name": "secret-value-canary"}), "/name"},
		"above maximum":              {encode(map[string]any{"count": 4}), "/count"},
		"below minimum":              {encode(map[string]any{"count": -1}), "/count"},
		"not an enum value":          {encode(map[string]any{"kind": "secret-value-canary"}), "/kind"},
		"not the const":              {encode(map[string]any{"fixed": 2}), "/fixed"},
		"null where forbidden":       {encode(map[string]any{"ratio": nil}), "/ratio"},
		"null outside the enum":      {encode(map[string]any{"color": nil}), "/color"},
		"null that is not the const": {encode(map[string]any{"only": nil}), "/only"},
		"huge exponent":              {encode(map[string]any{"count": json.Number("1e999999")}), "/count"},
		"huge negative exponent":     {encode(map[string]any{"ratio": json.Number("5e-401")}), "/ratio"},
		"long number literal":        {encode(map[string]any{"ratio": json.Number("0." + strings.Repeat("7", 255))}), "/ratio"},
		"bad date":                   {encode(map[string]any{"day": "2026-9-27"}), "/day"},
		"bad date-time":              {encode(map[string]any{"at": "yesterday"}), "/at"},
		"bad time":                   {encode(map[string]any{"clock": "25:00:00Z"}), "/clock"},
		"bad uuid":                   {encode(map[string]any{"id": "secret-value-canary"}), "/id"},
		"too many items":             {encode(map[string]any{"tags": []any{true, false, true}}), "/tags"},
		"wrong item type":            {encode(map[string]any{"tags": []any{"secret-value-canary"}}), "/tags/0"},
		"escaped pointer":            {encode(map[string]any{"a/b~c": map[string]any{"deep": "x"}}), "/a~1b~0c/deep"},
	}
	for name, failure := range failures {
		t.Run(name, func(t *testing.T) {
			err := validateStructuredOutputText(failure.text, schema)
			require.Error(t, err)
			require.True(t, strings.HasPrefix(err.Error(), "at "+failure.pointer+":"), err.Error())
			require.NotContains(t, err.Error(), "secret-value-canary")
		})
	}
}

func TestValidateStructuredOutputTextBoundsNumberParsing(t *testing.T) {
	schema := mustCanonicalSchema(t, objectSchema(map[string]any{
		"values": map[string]any{"type": "array", "items": map[string]any{"type": "integer"}},
	}))
	text := `{"values":[` + strings.TrimSuffix(strings.Repeat("1e999999,", 200), ",") + `]}`
	started := time.Now()
	err := validateStructuredOutputText(text, schema)
	// Elapsed time is the behavior: exact parsing of 1e999999 took about 55 ms per number.
	require.Less(t, time.Since(started), 2*time.Second)
	require.Error(t, err)
	require.Equal(t, "at /values/0: the number is out of range", err.Error())
}
