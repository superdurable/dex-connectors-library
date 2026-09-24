import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { SpreadsheetSetupView } from "../src/setup.js";

describe("Google Sheets setup", () => {
  it("shows resource selection only for a connected account", () => {
    const markup = renderToStaticMarkup(<SpreadsheetSetupView connection={{state: "connected", accountEmail: "owner@example.com", grantedScopes: []}} selection={{spreadsheetName: "Customers"}} tabs={["Important"]} onCommand={() => undefined}/>);
    expect(markup).toContain("Choose spreadsheet");
    expect(markup).toContain("owner@example.com");
    expect(markup).not.toContain("access_token");
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<SpreadsheetSetupView connection={{state, grantedScopes: []}} selection={{}} tabs={[]} onCommand={() => undefined}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("Reconnect");
    expect(markup).not.toContain("Choose spreadsheet");
  });
});
