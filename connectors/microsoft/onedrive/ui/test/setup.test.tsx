// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { OneDriveSetupView } from "../src/setup.js";
import { renderUnit, unitTarget } from "./render.js";

const siteID = "contoso.sharepoint.com,da60e844-ba1d-49bc-b4d4-d5e36bae9019,712a596e-90a1-49e3-9b48-bfa80bee8740";

describe("OneDrive connection surface", () => {
  it("shows the connected account without credential material", () => {
    const markup = renderToStaticMarkup(<OneDriveSetupView connection={{state: "connected", accountEmail: "megan@contoso.example", grantedScopes: ["Files.ReadWrite.All"]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain("megan@contoso.example");
    for (const secret of ["access_token", "refresh_token", "client_secret"]) expect(markup).not.toContain(secret);
  });

  it.each([["revoked", "Connection revoked"], ["insufficient_scope", "Additional permission required"]] as const)("offers reconnect for %s", (state, label) => {
    const markup = renderToStaticMarkup(<OneDriveSetupView connection={{state, grantedScopes: []}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("Reconnect Microsoft account");
  });

  it("explains the app-only method instead of offering OAuth consent", () => {
    const markup = renderToStaticMarkup(<OneDriveSetupView connection={{state: "not_configured", grantedScopes: [], authMethodIds: ["microsoft-app-only"]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain("App-only connection");
    expect(markup).toContain("every Step needs a drive ID");
    expect(markup).not.toContain("Connect Microsoft account");
  });
});

describe("site picker", () => {
  it("renders every field with search guidance, blank semantics, and the listed sites", () => {
    const markup = renderUnit({target: unitTarget("sitePicker", {value: {siteId: siteID, siteName: "Finance"}}), sites: {items: [{id: siteID, name: "Finance", webUrl: ""}], isTruncated: true}});
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain("leave blank to use your own OneDrive root.");
    expect(markup).toContain("Search SharePoint sites");
    expect(markup).toContain(">Search sites<");
    expect(markup).toContain("<strong>Site:</strong> Finance");
    expect(markup).toContain('<option value="">My OneDrive (no site)</option>');
    expect(markup).toContain(`<option value="${siteID}" selected="">Finance</option>`);
    expect(markup).toContain("as with Sites.Selected. Leave blank for your own OneDrive.");
    expect(markup).toContain("Microsoft Graph returned more sites than one list can show.");
    expect(markup).not.toContain("<style");
  });

  it("rejects a site ID that is not hostname,GUID,GUID", () => {
    const markup = renderUnit({target: unitTarget("sitePicker", {value: {siteId: "contoso.sharepoint.com"}})});
    expect(markup).toContain('role="alert">Enter a site ID of the form contoso.sharepoint.com,GUID,GUID.');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });

  it("reports an empty search and a failed one inline", () => {
    expect(renderUnit({target: unitTarget("sitePicker"), sites: {items: [], isTruncated: false}})).toContain("No site matched.");
    expect(renderUnit({target: unitTarget("sitePicker"), loadError: "Connector provider command failed"})).toContain('role="alert">Connector provider command failed');
  });
});
