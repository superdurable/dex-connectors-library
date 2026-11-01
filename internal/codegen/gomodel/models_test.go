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

func TestLoadJSONFieldRules(t *testing.T) {
	graph, err := Load(filepath.Join("testdata", "models"), []string{"Parent", "Recursive", "A", "B"})
	if err != nil {
		t.Fatal(err)
	}
	models := map[string]Model{}
	for _, model := range graph.Models {
		models[model.Name] = model
	}
	parent := models["Parent"]
	fields := map[string]Field{}
	for _, field := range parent.Fields {
		fields[field.Name] = field
	}
	for _, name := range []string{"promoted", "pointerPromoted", "tagged", "clash", "Winner", "omitEmpty", "omitZero", "nullablePointer", "nullableSlice", "nullableMap", "mode", "number", "plain", "anything", "inline", "when", "bytes", "raw"} {
		if _, ok := fields[name]; !ok {
			t.Errorf("missing %s", name)
		}
	}
	if _, ok := fields["ambiguous"]; ok {
		t.Error("ambiguous embedded field must be dropped")
	}
	if !fields["pointerPromoted"].Optional {
		t.Error("nil embedded pointer omits promoted field")
	}
	if field := fields["optional"]; !field.Optional || !field.Type.Nullable || !field.HasPresence {
		t.Errorf("promoted pointer may be omitted or encode null: %+v", field)
	}
	if fields["clash"].Type.Kind != Int {
		t.Error("shallow field must win")
	}
	if fields["Winner"].Type.Kind != String {
		t.Error("tagged field must win at equal depth")
	}
	for _, name := range []string{"omitEmpty", "omitZero"} {
		if !fields[name].Optional || fields[name].Type.Nullable {
			t.Errorf("%s must be optional and non-nullable: %+v", name, fields[name])
		}
	}
	for _, name := range []string{"nullablePointer", "nullableSlice", "nullableMap"} {
		if fields[name].Optional || !fields[name].Type.Nullable {
			t.Errorf("%s must be required and nullable: %+v", name, fields[name])
		}
	}
	if fields["nullableMap"].Type.Elem == nil || !fields["nullableMap"].Type.Elem.Nullable {
		t.Error("nullable map values lost")
	}
	if fields["mode"].Type.Kind != Enum || len(fields["mode"].Type.EnumValues) != 2 {
		t.Error("string enum lost")
	}
	if fields["number"].Type.Kind != Enum || len(fields["number"].Type.EnumValues) != 2 {
		t.Error("numeric enum lost")
	}
	if fields["plain"].Type.Kind != String || fields["anything"].Type.Kind != Unknown {
		t.Error("plain or alias type lost")
	}
	if fields["inline"].Type.Kind != Ref || models[fields["inline"].Type.Ref].Name == "" {
		t.Error("inline struct model missing")
	}
	if fields["when"].Type.Kind != Time || fields["bytes"].Type.Kind != Bytes || fields["raw"].Type.Kind != Unknown {
		t.Error("special mappings lost")
	}
	if models["Recursive"].Fields[0].Type.Ref != "Recursive" || models["A"].Fields[0].Type.Ref != "B" || models["B"].Fields[0].Type.Ref != "A" {
		t.Error("recursive refs missing")
	}
}

func TestUnsupportedJSONEncodingsFail(t *testing.T) {
	for _, test := range []struct{ name, want string }{{"UnsupportedOption", "unsupported json tag option"}, {"UnsupportedMarshaler", "custom JSON or text marshaler"}, {"UnsupportedTextMarshaler", "custom JSON or text marshaler"}, {"PromotedTime", "custom JSON or text marshaler"}, {"InlinePromotedTime", "custom JSON or text marshaler"}} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(filepath.Join("testdata", "models"), []string{test.name})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %s", err, test.want)
			}
		})
	}
}

func TestGoJSONShapeEdgeCases(t *testing.T) {
	graph, err := Load(filepath.Join("testdata", "models"), []string{"PointerSlice", "PointerPresence", "CommaTag", "GenericPages", "ExternalRecursive", "CountDepth"})
	if err != nil {
		t.Fatal(err)
	}
	models := map[string]Model{}
	for _, model := range graph.Models {
		models[model.Name] = model
	}
	if field := models["PointerSlice"].Fields[0]; !field.Optional || !field.Type.Nullable {
		t.Errorf("pointer to nil slice can encode null even with omitempty: %+v", field)
	}
	for _, field := range models["PointerPresence"].Fields {
		if !field.Optional || !field.HasPresence {
			t.Errorf("pointer omission must retain presence: %+v", field)
		}
		if isScalar := field.Name != "values"; isScalar && field.Type.Nullable {
			t.Errorf("scalar pointer with omit option cannot encode null: %+v", field)
		}
	}
	if field := models["CommaTag"].Fields[0]; field.Name != "v" {
		t.Errorf("trailing comma must retain JSON field name: %+v", field)
	}
	pages := models["GenericPages"].Fields
	if pages[0].Type.Ref == pages[1].Type.Ref || models[pages[0].Type.Ref].Name == "" || models[pages[1].Type.Ref].Name == "" {
		t.Errorf("generic instantiations collapsed: %+v", pages)
	}
	root := models["ExternalRecursive"].Fields[0].Type.Ref
	if root == "" || models[root].Fields[0].Type.Ref != root {
		t.Errorf("external recursive model missing: %s %+v", root, models[root])
	}
	if fields := models["CountDepth"].Fields; len(fields) != 1 || fields[0].Name != "z" {
		t.Errorf("repeated embedding must not multiply deeper field counts: %+v", fields)
	}
	type deepZ struct {
		Z int `json:"z"`
	}
	type sharedMiddle struct{ deepZ }
	type leftMiddle struct{ sharedMiddle }
	type rightMiddle struct{ sharedMiddle }
	encoded, err := json.Marshal(struct {
		leftMiddle
		rightMiddle
	}{})
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"z":0}` {
		t.Fatalf("encoding/json embedded-depth result: %s", encoded)
	}
	value := struct {
		Values *[]string `json:"values,omitempty"`
	}{Values: new([]string)}
	encoded, err = json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"values":null}` {
		t.Fatalf("encoding/json returned %s", encoded)
	}
	zeroBool, zeroInt, zeroString := false, 0, ""
	withPresence := struct {
		Bool   *bool     `json:"bool,omitempty"`
		Number *int      `json:"number,omitempty"`
		Text   *string   `json:"text,omitzero"`
		Values *[]string `json:"values,omitempty"`
	}{Bool: &zeroBool, Number: &zeroInt, Text: &zeroString, Values: new([]string)}
	encoded, err = json.Marshal(withPresence)
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{"bool":false,"number":0,"text":"","values":null}` {
		t.Fatalf("encoding/json lost pointer presence: %s", encoded)
	}
}

func TestDistinctEmbeddedAndExternalTypes(t *testing.T) {
	graph, err := Load(filepath.Join("testdata", "models"), []string{"ExternalSameName", "DistinctEmbeddings", "NamedBytes"})
	if err != nil {
		t.Fatal(err)
	}
	models := map[string]Model{}
	for _, model := range graph.Models {
		models[model.Name] = model
	}
	external := models["ExternalSameName"].Fields
	if external[0].Type.Ref == external[1].Type.Ref {
		t.Fatalf("external package paths collapsed: %+v", external)
	}
	if len(models[external[0].Type.Ref].Fields) != 1 || models[external[0].Type.Ref].Fields[0].Name != "alpha" || len(models[external[1].Type.Ref].Fields) != 1 || models[external[1].Type.Ref].Fields[0].Name != "beta" {
		t.Fatalf("external model fields mixed: %+v %+v", models[external[0].Type.Ref], models[external[1].Type.Ref])
	}
	type SameFields struct {
		Z int `json:"z"`
	}
	type OtherSameFields SameFields
	structure := reflect.StructOf([]reflect.StructField{
		{Name: "SameFields", Type: reflect.TypeOf(SameFields{}), Anonymous: true},
		{Name: "OtherSameFields", Type: reflect.TypeOf(OtherSameFields{}), Anonymous: true},
	})
	encoded, err := json.Marshal(reflect.New(structure).Elem().Interface())
	if err != nil {
		t.Fatal(err)
	}
	if string(encoded) != `{}` || len(models["DistinctEmbeddings"].Fields) != 0 {
		t.Fatalf("distinct embedded types must conflict like encoding/json: %s %+v", encoded, models["DistinctEmbeddings"].Fields)
	}
	if field := models["NamedBytes"].Fields[0]; field.Type.Kind != Bytes {
		t.Fatalf("named byte slice must be base64: %+v", field)
	}
	type namedByte byte
	encoded, err = json.Marshal(struct {
		Value []namedByte `json:"value"`
	}{Value: []namedByte{0, 1}})
	if err != nil || string(encoded) != `{"value":"AAE="}` {
		t.Fatalf("encoding/json byte baseline: %s %v", encoded, err)
	}
}
