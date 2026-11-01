// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package gomodel

import (
	"path/filepath"
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
	for _, test := range []struct{ name, want string }{{"UnsupportedOption", "unsupported json tag option"}, {"UnsupportedMarshaler", "custom JSON or text marshaler"}, {"UnsupportedTextMarshaler", "custom JSON or text marshaler"}} {
		t.Run(test.name, func(t *testing.T) {
			_, err := Load(filepath.Join("testdata", "models"), []string{test.name})
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("got %v, want %s", err, test.want)
			}
		})
	}
}
