// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { SpreadsheetSetupView } from "../src/setup.js";
import { SpreadsheetConfigurationUnit } from "../src/units.js";

describe("Google Sheets setup", () => {
  it("shows resource selection only for a connected account", () => {
    const markup = renderToStaticMarkup(<SpreadsheetSetupView connection={{state: "connected", accountEmail: "owner@example.com", grantedScopes: []}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).not.toContain("Choose spreadsheet");
    expect(markup).toContain("owner@example.com");
    expect(markup).not.toContain("access_token");
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<SpreadsheetSetupView connection={{state, grantedScopes: []}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("Reconnect");
    expect(markup).not.toContain("Choose spreadsheet");
  });

  it("renders spreadsheet selection as an isolated unit", () => {
    const markup = renderToStaticMarkup(<SpreadsheetConfigurationUnit target={{kind: "configurationUnit", scope: {kind: "operation", operationId: "findRow", flowType: "ImportFlow", stepType: "FindCustomer"}, instanceId: "sheet", unitId: "spreadsheetPicker", label: "Customer spreadsheet", required: true, bindings: [{port: "spreadsheetId", jsonPointer: "/spreadsheetId"}], value: {spreadsheetName: "Customers", spreadsheetId: "sheet-1"}}} tabs={[]} onChooseSpreadsheet={() => undefined} onLoadTabs={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain("Choose spreadsheet");
    expect(markup).toContain("Customers");
    expect(markup).not.toContain("more spreadsheets than one list can show");
  });

  it("points to the spreadsheet ID field when Drive returns more spreadsheets than the list shows", () => {
    const markup = renderToStaticMarkup(<SpreadsheetConfigurationUnit isSpreadsheetListTruncated spreadsheets={[{id: "sheet-1", name: "Customers"}]} target={{kind: "configurationUnit", scope: {kind: "operation", operationId: "findRow", flowType: "ImportFlow", stepType: "FindCustomer"}, instanceId: "sheet", unitId: "spreadsheetPicker", label: "Customer spreadsheet", required: true, bindings: [{port: "spreadsheetId", jsonPointer: "/spreadsheetId"}], value: {}}} tabs={[]} onChooseSpreadsheet={() => undefined} onLoadTabs={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain('role="status">Google Drive returned more spreadsheets than one list can show. Enter a spreadsheet ID to use one that is not listed.');
  });

  it("shows a load error inline and lists tabs only for a saved spreadsheet", () => {
    const markup = renderToStaticMarkup(<SpreadsheetConfigurationUnit target={{kind: "configurationUnit", scope: {kind: "operation", operationId: "findRow", flowType: "ImportFlow", stepType: "FindCustomer"}, instanceId: "tab", unitId: "sheetTabPicker", label: "Customer tab", required: true, bindings: [{port: "tab", jsonPointer: "/tab"}], value: {}}} loadError="The caller does not have permission" tabs={[]} onChooseSpreadsheet={() => undefined} onLoadTabs={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain('role="alert">The caller does not have permission');
    expect(markup).toContain('type="button" disabled="">Load tabs');
    expect(markup).toContain("Save a spreadsheet first to list its tabs.");
  });
});
