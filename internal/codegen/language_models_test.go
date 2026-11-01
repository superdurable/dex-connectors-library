// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadGoModelsFollowsJSONStructFields(t *testing.T) {
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "go.mod"), []byte("module example.com/models\n\ngo 1.24.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	source := `package models
import "time"
type Child struct { Name string ` + "`json:\"name\"`" + ` }
type Parent struct {
  Child Child ` + "`json:\"child\"`" + `
  Optional *string ` + "`json:\"optional\"`" + `
  Items []Child ` + "`json:\"items,omitempty\"`" + `
  Properties map[string]string ` + "`json:\"properties\"`" + `
  CreatedAt time.Time ` + "`json:\"createdAt\"`" + `
}`
	if err := os.WriteFile(filepath.Join(directory, "models.go"), []byte(source), 0o644); err != nil {
		t.Fatal(err)
	}
	models, err := LoadGoModels(directory, []string{"Parent"})
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].Name != "Child" || models[1].Name != "Parent" {
		t.Fatalf("nested models must precede their parent: %+v", models)
	}
	fields := models[1].Fields
	if len(fields) != 5 || fields[0].Zod != "ChildSchema" || fields[1].Name != "optional" || !fields[1].Optional || fields[2].Zod != "z.array(ChildSchema)" || !fields[2].Optional || fields[3].Zod != "z.record(z.string(), z.string())" || fields[4].Zod != "z.iso.datetime({ offset: true })" {
		t.Fatalf("unexpected mapped Go fields: %+v", fields)
	}
}
