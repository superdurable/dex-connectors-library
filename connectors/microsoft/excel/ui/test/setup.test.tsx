// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { ExcelSetupView } from "../src/setup.js";
import { ExcelConfigurationUnit, type ExcelUnitProps } from "../src/units.js";

const noop = () => undefined;

function unitTarget(unitId: string, value: Record<string, unknown>): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit",
    scope: {kind: "operation", operationId: "getTableRows", flowType: "MicrosoftExcelApprovalDecision", stepType: "ReadExcelApprovalPolicy"},
    instanceId: unitId === "workbookPicker" ? "policyWorkbook" : "policyTable", unitId,
    label: unitId === "workbookPicker" ? "Approval policy workbook" : "Approval policy table",
    description: "Choose the approval-policy table; the picker stores its ID.",
    required: true,
    bindings: [{port: "driveId", jsonPointer: "/driveId"}, {port: "workbookId", jsonPointer: "/workbookId"}],
    value,
  };
}

function renderUnit(props: Partial<ExcelUnitProps> & {target: ConnectorStudioConfigurationUnitTarget}): string {
  return renderToStaticMarkup(<ExcelConfigurationUnit
    onLoadTables={noop} onLoadWorksheets={noop} onResolveSharingLink={noop} onSave={noop} onSearchWorkbooks={noop} {...props}
  />);
}

describe("Microsoft Excel connection surface", () => {
  it("confirms the connection without rendering any credential", () => {
    const markup = renderToStaticMarkup(<ExcelSetupView connection={{state: "connected", grantedScopes: ["Files.ReadWrite.All"]}} onConnect={noop} onReconnect={noop}/>);
    expect(markup).toContain("Connected");
    for (const secret of ["access_token", "refresh_token", "oauth_client_secret", "Bearer"]) expect(markup).not.toContain(secret);
  });

  it("names the requested permissions before authorization", () => {
    const markup = renderToStaticMarkup(<ExcelSetupView connection={{state: "not_configured", grantedScopes: []}} onConnect={noop} onReconnect={noop}/>);
    expect(markup).toContain("Files.ReadWrite.All");
    expect(markup).toContain("offline_access");
    expect(markup).toContain("Connect Microsoft Excel");
  });

  it.each(["expired", "revoked", "insufficient_scope"] as const)("offers reconnection when the connection is %s", (state) => {
    expect(renderToStaticMarkup(<ExcelSetupView connection={{state, grantedScopes: []}} onConnect={noop} onReconnect={noop}/>))
      .toContain("Reconnect Microsoft Excel");
  });
});

describe("Microsoft Excel configuration units", () => {
  it("lists found workbooks, explains sharing links, and saves only valid IDs", () => {
    const markup = renderUnit({
      target: unitTarget("workbookPicker", {driveId: "b!drive", workbookId: "01ITEM", workbookName: "Approvals.xlsx"}),
      workbooks: [{driveId: "b!drive", workbookId: "01ITEM", name: "Approvals.xlsx"}],
    });
    expect(markup).toContain("Search my workbooks");
    expect(markup).toContain('<option value="b!drive/01ITEM" selected="">Approvals.xlsx</option>');
    expect(markup).toContain("Share &gt; Copy link");
    expect(markup).toContain("<strong>Workbook:</strong> Approvals.xlsx");
    expect(markup).not.toContain('disabled="">Save');

    const invalid = renderUnit({target: unitTarget("workbookPicker", {driveId: "b!drive", workbookId: "bad id"}), isWorkbookListTruncated: true});
    expect(invalid).toContain('disabled="">Save');
    expect(invalid).toContain("only letters, digits");
    expect(invalid).toContain("more workbooks than one list can show");
  });

  it("lists tables only after a business-drive workbook is saved and keeps manual entry", () => {
    const withoutWorkbook = renderUnit({target: unitTarget("tablePicker", {}), loadError: "Connector provider command failed"});
    expect(withoutWorkbook).toContain('type="button" disabled="">Load tables');
    expect(withoutWorkbook).toContain("Save a workbook first to list its tables.");
    expect(withoutWorkbook).toContain('role="alert">Connector provider command failed');

    const listed = renderUnit({
      target: unitTarget("tablePicker", {driveId: "b!drive", workbookId: "01ITEM", table: "{T1}", tableName: "ApprovalPolicy"}),
      tables: [{id: "{T1}", name: "ApprovalPolicy"}, {id: "{T2}", name: "Decisions"}],
    });
    expect(listed).not.toContain('disabled="">Load tables');
    expect(listed).toContain('<option value="{T1}" selected="">ApprovalPolicy</option>');
    expect(listed).toContain("Table Design &gt; Table Name");

    const personalDrive = renderUnit({target: unitTarget("worksheetPicker", {driveId: "1a2b3c", workbookId: "01ITEM"})});
    expect(personalDrive).toContain('disabled="">Load worksheets');
    expect(personalDrive).toContain("Type the worksheet name");
  });

  it("reports an unknown unit instead of rendering a field", () => {
    expect(renderUnit({target: unitTarget("chartPicker", {})})).toContain("Unsupported Microsoft Excel configuration unit: chartPicker");
  });
});
