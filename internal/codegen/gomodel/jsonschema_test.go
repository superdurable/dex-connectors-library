// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gomodel

import (
	"encoding/json"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestJSONSchemaUsesGoJSONNamesAndOmission(t *testing.T) {
	graph, err := Load(filepath.Join("testdata", "models"), []string{"Parent"})
	if err != nil {
		t.Fatal(err)
	}
	document, err := graph.JSONSchema("Parent")
	if err != nil {
		t.Fatal(err)
	}
	properties := document["properties"].(map[string]any)
	models := map[string]Model{}
	for _, model := range graph.Models {
		models[model.Name] = model
	}
	var expectedRequired []string
	for _, field := range models["Parent"].Fields {
		if _, ok := properties[field.Name]; !ok {
			t.Errorf("schema lacks JSON field %s", field.Name)
		}
		if !field.Optional {
			expectedRequired = append(expectedRequired, field.Name)
		}
	}
	if len(properties) != len(models["Parent"].Fields) {
		t.Errorf("schema has %d properties for %d fields", len(properties), len(models["Parent"].Fields))
	}
	required := document["required"].([]string)
	for _, name := range expectedRequired {
		if !strings.Contains(" "+strings.Join(required, " ")+" ", " "+name+" ") {
			t.Errorf("%s must be required", name)
		}
	}
	for _, name := range []string{"omitEmpty", "omitZero", "pointerPromoted", "optional"} {
		if strings.Contains(" "+strings.Join(required, " ")+" ", " "+name+" ") {
			t.Errorf("%s may be omitted", name)
		}
	}
	if document["additionalProperties"] != false {
		t.Error("unknown properties must be rejected")
	}
	if _, ok := properties["ambiguous"]; ok {
		t.Error("a field that Go drops must stay out of the schema")
	}
}

func TestJSONSchemaKinds(t *testing.T) {
	stringType := Type{Kind: String}
	graph := Graph{Models: []Model{
		{Name: "Root", Comment: "Root is one\nrequest.", Fields: []Field{
			{Name: "text", Type: stringType, Comment: "Text is\n  the body."},
			{Name: "flag", Type: Type{Kind: Bool}},
			{Name: "count", Type: Type{Kind: Int}},
			{Name: "large", Type: Type{Kind: Int64}},
			{Name: "ratio", Type: Type{Kind: Float}},
			{Name: "when", Type: Type{Kind: Time}},
			{Name: "blob", Type: Type{Kind: Bytes, Nullable: true}},
			{Name: "raw", Type: Type{Kind: Unknown, Nullable: true}},
			{Name: "names", Type: Type{Kind: List, Elem: &stringType, Nullable: true}},
			{Name: "labels", Type: Type{Kind: Map, Key: &stringType, Elem: &stringType}, Optional: true},
			{Name: "mode", Type: Type{Kind: Enum, Name: "Mode", EnumBase: String, EnumValues: []string{"fast", "slow"}}},
			{Name: "level", Type: Type{Kind: Enum, Name: "Level", EnumBase: Int, EnumValues: []string{"1", "2"}, Nullable: true}},
			{Name: "child", Type: Type{Kind: Ref, Ref: "Child", Nullable: true}, Optional: true},
		}},
		{Name: "Child", Fields: []Field{{Name: "parent", Type: Type{Kind: Ref, Ref: "Root"}, Optional: true}}},
		{Name: "Unreached", Fields: []Field{{Name: "value", Type: stringType}}},
	}}
	document, err := graph.JSONSchema("Root")
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	properties := decoded["properties"].(map[string]any)
	for name, expected := range map[string]string{
		"text":   `{"type":"string","description":"Text is the body."}`,
		"flag":   `{"type":"boolean"}`,
		"count":  `{"type":"integer"}`,
		"large":  `{"type":"integer"}`,
		"ratio":  `{"type":"number"}`,
		"when":   `{"type":"string","format":"date-time"}`,
		"blob":   `{"type":["string","null"],"contentEncoding":"base64"}`,
		"raw":    `{}`,
		"names":  `{"type":["array","null"],"items":{"type":"string"}}`,
		"labels": `{"type":"object","additionalProperties":{"type":"string"}}`,
		"mode":   `{"type":"string","enum":["fast","slow"]}`,
		"level":  `{"type":["integer","null"],"enum":[1,2,null]}`,
		"child":  `{"anyOf":[{"$ref":"#/$defs/Child"},{"type":"null"}]}`,
	} {
		var want any
		if err := json.Unmarshal([]byte(expected), &want); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(properties[name], want) {
			t.Errorf("%s = %v, want %v", name, properties[name], want)
		}
	}
	if decoded["description"] != "Root is one request." {
		t.Errorf("description = %v", decoded["description"])
	}
	definitions := decoded["$defs"].(map[string]any)
	if _, ok := definitions["Root"]; !ok {
		t.Error("a recursive root must also be a definition")
	}
	if _, ok := definitions["Unreached"]; ok {
		t.Error("an unreachable model must stay out of the document")
	}
}

func TestJSONSchemaOfEmptyModelHasNoRequiredList(t *testing.T) {
	document, err := Graph{Models: []Model{{Name: "Empty"}}}.JSONSchema("Empty")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := document["required"]; ok {
		t.Error("an empty required list is noise for a schema reader")
	}
	if _, ok := document["properties"]; !ok {
		t.Error("an object schema always has properties")
	}
}

func TestJSONSchemaFailsOnMissingModels(t *testing.T) {
	if _, err := (Graph{}).JSONSchema("Missing"); err == nil {
		t.Error("a missing root must fail")
	}
	graph := Graph{Models: []Model{{Name: "Root", Fields: []Field{{Name: "next", Type: Type{Kind: Ref, Ref: "Missing"}}}}}}
	if _, err := graph.JSONSchema("Root"); err == nil {
		t.Error("a missing reference must fail")
	}
}
