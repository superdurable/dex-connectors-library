// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gomodel

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
)

const jsonSchemaDefinitionPrefix = "#/$defs/"

// JSONSchema returns a self-contained JSON Schema draft 2020-12 object for one model in the graph.
//
// Property names are the JSON names that the Go, Python, and TypeScript models all encode. A field
// is required unless Go omits it when empty, and unknown properties are rejected, which matches the
// strict TypeScript validators. The root model is inline so the document can be used as a tool input
// schema; other models are referenced through $defs. Enum constants are listed even though the
// language models accept other values, because a schema reader must choose one of the known values.
func (graph Graph) JSONSchema(rootName string) (map[string]any, error) {
	modelsByName := make(map[string]Model, len(graph.Models))
	for _, model := range graph.Models {
		modelsByName[model.Name] = model
	}
	root, ok := modelsByName[rootName]
	if !ok {
		return nil, fmt.Errorf("Go model %s is missing from the model graph", rootName)
	}
	builder := jsonSchemaBuilder{models: modelsByName, definitions: map[string]any{}}
	document, err := builder.objectSchema(root)
	if err != nil {
		return nil, err
	}
	for len(builder.pending) > 0 {
		name := builder.pending[0]
		builder.pending = builder.pending[1:]
		definition, err := builder.objectSchema(modelsByName[name])
		if err != nil {
			return nil, err
		}
		builder.definitions[name] = definition
	}
	if len(builder.definitions) > 0 {
		document["$defs"] = builder.definitions
	}
	return document, nil
}

type jsonSchemaBuilder struct {
	models      map[string]Model
	definitions map[string]any
	pending     []string
}

func (builder *jsonSchemaBuilder) objectSchema(model Model) (map[string]any, error) {
	properties := make(map[string]any, len(model.Fields))
	required := make([]string, 0, len(model.Fields))
	for _, field := range model.Fields {
		property, err := builder.typeSchema(field.Type)
		if err != nil {
			return nil, fmt.Errorf("Go model %s.%s: %w", model.Name, field.Name, err)
		}
		if description := singleLineComment(field.Comment); description != "" {
			property["description"] = description
		}
		properties[field.Name] = property
		if !field.Optional {
			required = append(required, field.Name)
		}
	}
	document := map[string]any{"type": "object", "properties": properties, "additionalProperties": false}
	if description := singleLineComment(model.Comment); description != "" {
		document["description"] = description
	}
	if len(required) > 0 {
		slices.Sort(required)
		document["required"] = required
	}
	return document, nil
}

func (builder *jsonSchemaBuilder) typeSchema(value Type) (map[string]any, error) {
	var document map[string]any
	switch value.Kind {
	case String:
		document = map[string]any{"type": "string"}
	case Bool:
		document = map[string]any{"type": "boolean"}
	case Int, Int64:
		document = map[string]any{"type": "integer"}
	case Float:
		document = map[string]any{"type": "number"}
	case Time:
		document = map[string]any{"type": "string", "format": "date-time"}
	case Bytes:
		document = map[string]any{"type": "string", "contentEncoding": "base64"}
	case Unknown:
		return map[string]any{}, nil
	case List:
		items, err := builder.typeSchema(*value.Elem)
		if err != nil {
			return nil, err
		}
		document = map[string]any{"type": "array", "items": items}
	case Map:
		values, err := builder.typeSchema(*value.Elem)
		if err != nil {
			return nil, err
		}
		document = map[string]any{"type": "object", "additionalProperties": values}
	case Ref:
		reference, err := builder.reference(value.Ref)
		if err != nil {
			return nil, err
		}
		if value.Nullable {
			return map[string]any{"anyOf": []any{reference, map[string]any{"type": "null"}}}, nil
		}
		return reference, nil
	case Enum:
		var err error
		if document, err = enumSchema(value); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("unsupported model kind %q", value.Kind)
	}
	if value.Nullable {
		document["type"] = []any{document["type"], "null"}
		if values, ok := document["enum"].([]any); ok {
			document["enum"] = append(values, nil)
		}
	}
	return document, nil
}

func (builder *jsonSchemaBuilder) reference(name string) (map[string]any, error) {
	if _, ok := builder.models[name]; !ok {
		return nil, fmt.Errorf("Go model %s is missing from the model graph", name)
	}
	if _, seen := builder.definitions[name]; !seen {
		builder.definitions[name] = nil
		builder.pending = append(builder.pending, name)
	}
	return map[string]any{"$ref": jsonSchemaDefinitionPrefix + name}, nil
}

func enumSchema(value Type) (map[string]any, error) {
	values := make([]any, 0, len(value.EnumValues))
	if value.EnumBase == String {
		for _, enumValue := range value.EnumValues {
			values = append(values, enumValue)
		}
		return map[string]any{"type": "string", "enum": values}, nil
	}
	for _, enumValue := range value.EnumValues {
		number, err := strconv.ParseInt(enumValue, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("enum %s value %q: %w", value.Name, enumValue, err)
		}
		values = append(values, number)
	}
	return map[string]any{"type": "integer", "enum": values}, nil
}

func singleLineComment(comment string) string {
	return strings.Join(strings.Fields(comment), " ")
}
