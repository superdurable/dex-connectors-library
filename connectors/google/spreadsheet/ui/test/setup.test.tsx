import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { SpreadsheetSetupView } from "../src/setup.js";

describe("Google Sheets setup", () => {
  it("shows resource selection only for a connected account", () => {
    const markup = renderToStaticMarkup(<SpreadsheetSetupView connection={{state: "connected", accountEmail: "owner@example.com", grantedScopes: []}} selection={{spreadsheetName: "Customers"}} tabs={["Important"]} capabilities={["google.picker.spreadsheets", "google.sheets.tabs-list"]} onCommand={() => undefined}/>);
    expect(markup).toContain("Choose spreadsheet");
    expect(markup).toContain("owner@example.com");
    expect(markup).not.toContain("access_token");
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<SpreadsheetSetupView connection={{state, grantedScopes: []}} selection={{}} tabs={[]} capabilities={[]} onCommand={() => undefined}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("Reconnect");
    expect(markup).not.toContain("Choose spreadsheet");
  });

  it("disables commands that the Dex host does not support", () => {
    const markup = renderToStaticMarkup(<SpreadsheetSetupView connection={{state: "connected", grantedScopes: []}} selection={{}} tabs={[]} capabilities={[]} onCommand={() => undefined}/>);
    expect(markup).toContain("Spreadsheet Picker is unavailable");
    expect(markup).toContain("Tab discovery is unavailable");
    expect(markup).toContain("disabled");
  });
});
