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
