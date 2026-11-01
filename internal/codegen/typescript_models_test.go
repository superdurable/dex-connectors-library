// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package codegen

import (
	"strings"
	"testing"

	"github.com/superdurable/dex-connectors-library/internal/codegen/gomodel"
	"github.com/superdurable/dex-connectors-library/schema"
)

func TestTypeScriptModelsKeepRequiredFieldsAndLimitLazyToCycles(t *testing.T) {
	models := []gomodel.Model{
		{Name: "Plain", Fields: []gomodel.Field{{Name: "id", Type: gomodel.Type{Kind: gomodel.String}}, {Name: "status", Type: gomodel.Type{Kind: gomodel.Enum, EnumBase: gomodel.String, EnumValues: []string{"open", "closed"}}}}},
		{Name: "A", Fields: []gomodel.Field{{Name: "b", Type: gomodel.Type{Kind: gomodel.Ref, Ref: "B", Nullable: true}}}},
		{Name: "B", Fields: []gomodel.Field{{Name: "a", Type: gomodel.Type{Kind: gomodel.Ref, Ref: "A", Nullable: true}}}},
	}
	generated := string(generateTypeScriptModels(schema.Manifest{}, models))
	if !strings.Contains(generated, "export const PlainSchema = z.strictObject({") || strings.Contains(generated, ".prefault(") {
		t.Fatalf("plain schema lost its required ZodObject shape:\n%s", generated)
	}
	if !strings.Contains(generated, "\"status\": z.string(),") || !strings.Contains(generated, `"open" | "closed" | (string & {})`) {
		t.Fatalf("named string enum is not open:\n%s", generated)
	}
	if strings.Count(generated, "= z.lazy(") != 2 {
		t.Fatalf("only the mutually recursive models should be lazy:\n%s", generated)
	}
	if got := emptyTypeScriptValue(gomodel.Type{Kind: gomodel.Enum, EnumBase: gomodel.String, EnumValues: []string{"open"}}); got != "''" {
		t.Fatalf("enum zero value fabricated a known status: %s", got)
	}
}

func TestTypeScriptManifestDescriptionsAreSafeJSDoc(t *testing.T) {
	manifest := schema.Manifest{Spec: schema.Spec{
		Configuration: schema.Configuration{Fields: []schema.Field{{Name: "timeout", Type: "duration", Description: "Go duration */ unsafe"}, {Name: "label", Type: "string", Description: "Label */ unsafe"}}},
		Auth:          schema.Auth{Fields: []schema.Field{{Name: "token", GoName: "Token", Type: "string", Description: "Token */ unsafe", Required: true}}},
	}}
	generated := string(generateTypeScriptModels(manifest, nil))
	if strings.Contains(generated, "*/ unsafe") || !strings.Contains(generated, "Duration in milliseconds for timeoutMs") || !strings.Contains(generated, "Label * / unsafe") || !strings.Contains(generated, "Token * / unsafe") {
		t.Fatalf("unsafe or misleading generated JSDoc:\n%s", generated)
	}
	steps := string(generateTypeScriptSteps([]schema.Operation{{Name: "read", GoName: "Read", InputType: "Input", OutputType: "Output", Branches: []schema.OperationBranch{{ID: "done", Description: "Done */ unsafe"}}}}))
	if strings.Contains(steps, "*/ unsafe") || !strings.Contains(steps, "Done * / unsafe") {
		t.Fatalf("branch JSDoc was not escaped:\n%s", steps)
	}
}
