// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { AirtableSetupView } from "../src/setup.js";
import { AirtableConfigurationUnit } from "../src/units.js";

const ignore = () => undefined;

function unitTarget(unitId: string, value: Record<string, unknown>): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit",
    scope: {kind: "operation", operationId: "listRecords", flowType: "AirtableRefundDecision", stepType: "FindAirtableRefundPolicy"},
    instanceId: unitId === "basePicker" ? "policyBase" : "policyTable", unitId,
    label: unitId === "basePicker" ? "Policy base" : "Policy table",
    description: "Select the refund policy base and table; the picker stores their IDs.",
    required: true,
    bindings: unitId === "basePicker"
      ? [{port: "baseId", jsonPointer: "/baseId"}, {port: "baseName", jsonPointer: "/baseName"}]
      : [{port: "baseId", jsonPointer: "/baseId"}, {port: "tableId", jsonPointer: "/tableId"}, {port: "tableName", jsonPointer: "/tableName"}],
    value,
  };
}

describe("Airtable connection surface", () => {
  it("confirms a saved token without rendering any credential", () => {
    const markup = renderToStaticMarkup(<AirtableSetupView connection={{state: "connected", grantedScopes: [], authMethodIds: []}}/>);
    expect(markup).toContain("Connected with a personal access token.");
    expect(markup).not.toContain("personal_access_token");
    expect(markup).not.toContain("Bearer");
  });

  it("tells a new connection which scopes the token needs", () => {
    const markup = renderToStaticMarkup(<AirtableSetupView connection={{state: "not_configured", grantedScopes: []}}/>);
    for (const scope of ["data.records:read", "data.records:write", "schema.bases:read"]) expect(markup).toContain(scope);
    expect(markup).toContain("never sends it to this panel");
  });

  it.each(["expired", "revoked", "insufficient_scope"] as const)("asks for a current token when the connection is %s", (state) => {
    const markup = renderToStaticMarkup(<AirtableSetupView connection={{state, grantedScopes: []}}/>);
    expect(markup).toContain("Paste a current personal access token");
  });
});

describe("Airtable configuration units", () => {
  it("lists loaded bases and saves only a valid base ID", () => {
    const markup = renderToStaticMarkup(<AirtableConfigurationUnit
      bases={[{id: "appRefundBase0001", name: "Refunds", permissionLevel: "create"}]}
      onLoadBases={ignore} onLoadTables={ignore} onSave={ignore}
      target={unitTarget("basePicker", {baseId: "appRefundBase0001", baseName: "Refunds"})}
    />);
    expect(markup).toContain("Load bases");
    expect(markup).toContain('<option value="appRefundBase0001" selected="">Refunds</option>');
    expect(markup).toContain("app followed by 14 letters and digits");
    expect(markup).toContain(">Save</button>");
    expect(markup).not.toContain('disabled="">Save');
    expect(markup).not.toContain("more bases than one list can show");
  });

  it("rejects a malformed base ID before saving and points to manual entry for a truncated list", () => {
    const markup = renderToStaticMarkup(<AirtableConfigurationUnit
      isBaseListTruncated onLoadBases={ignore} onLoadTables={ignore} onSave={ignore}
      target={unitTarget("basePicker", {baseId: "base-1"})}
    />);
    expect(markup).toContain("A base ID starts with app followed by 14 letters and digits.");
    expect(markup).toContain('disabled="">Save');
    expect(markup).toContain("Airtable returned more bases than one list can show.");
  });

  it("lists tables only after a base is saved and shows load errors inline", () => {
    const withoutBase = renderToStaticMarkup(<AirtableConfigurationUnit
      loadError="Connector provider command failed" onLoadBases={ignore} onLoadTables={ignore} onSave={ignore}
      target={unitTarget("tablePicker", {})}
    />);
    expect(withoutBase).toContain('type="button" disabled="">Load tables');
    expect(withoutBase).toContain("Save a base first to list its tables.");
    expect(withoutBase).toContain('role="alert">Connector provider command failed');

    const withBase = renderToStaticMarkup(<AirtableConfigurationUnit
      onLoadBases={ignore} onLoadTables={ignore} onSave={ignore}
      tables={[{id: "tblPolicies000001", name: "Policies"}, {id: "tblDecisionLog001", name: "Decision Log"}]}
      target={unitTarget("tablePicker", {baseId: "appRefundBase0001", tableId: "tblPolicies000001", tableName: "Policies"})}
    />);
    expect(withBase).not.toContain('disabled="">Load tables');
    expect(withBase).toContain('<option value="tblPolicies000001" selected="">Policies</option>');
    expect(withBase).toContain("<strong>Table:</strong> Policies");
    expect(withBase).toContain('class="studio-surface"');
  });

  it("reports an unknown unit instead of rendering a field", () => {
    const markup = renderToStaticMarkup(<AirtableConfigurationUnit onLoadBases={ignore} onLoadTables={ignore} onSave={ignore} target={unitTarget("fieldPicker", {})}/>);
    expect(markup).toContain("Unsupported Airtable configuration unit: fieldPicker");
  });
});
