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
  });
});
