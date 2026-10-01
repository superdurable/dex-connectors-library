// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { ConnectorStudioConnectionTarget } from "@superdurable/dex-connectors-react";
import { ZohoDeskSetupView } from "../src/setup.js";
import { OrganizationPickerUnit } from "../src/units.js";

const pickerTarget: ConnectorStudioConnectionTarget = {kind: "connection", unitId: "organizationPicker", bindings: [{port: "orgId", jsonPointer: "/orgId"}]};

describe("Zoho Desk setup", () => {
  it("shows the connected account without any credential", () => {
    const markup = renderToStaticMarkup(<ZohoDeskSetupView connection={{state: "connected", accountEmail: "ada@example.com", grantedScopes: [
      "Desk.tickets.READ", "Desk.tickets.CREATE", "Desk.tickets.UPDATE", "Desk.search.READ", "Desk.basic.READ",
    ]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain("ada@example.com");
    expect(markup).toContain("Choose the Zoho Desk organization in the connection form.");
    expect(markup).not.toContain("access_token");
    expect(markup).not.toContain("refresh_token");
    expect(markup).not.toContain("client_secret");
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<ZohoDeskSetupView connection={{state, grantedScopes: []}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("accept every requested scope");
    expect(markup).toContain("Reconnect Zoho Desk");
  });

  it("renders listed organizations, manual orgId guidance, and validation", () => {
    const markup = renderToStaticMarkup(<OrganizationPickerUnit canListOrganizations onChooseOrganization={() => undefined} onSave={() => undefined}
      organizations={[{orgId: "3981311", companyName: "Zylker INC.", portalName: "zylker", isSandbox: false}]}
      target={{...pickerTarget, value: {orgId: "zylker"}}}/>);
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain("the one you picked on the Zoho consent screen");
    expect(markup).toContain("Choose Zoho Desk organization");
    expect(markup).toContain("Zylker INC. (zylker), orgId 3981311");
    expect(markup).toContain("Setup &gt; Developer Space &gt; API");
    expect(markup).toContain('role="alert">The orgId must be the organization&#x27;s numeric ID');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
    expect(markup).not.toContain("<style");
  });

  it("offers only manual entry when the host reports no data center", () => {
    const markup = renderToStaticMarkup(<OrganizationPickerUnit canListOrganizations={false} onChooseOrganization={() => undefined} onSave={() => undefined}
      target={{...pickerTarget, value: {orgId: "2389290"}}}/>);
    expect(markup).toContain("Dex Web did not report the connection&#x27;s data center");
    expect(markup).not.toContain("Choose Zoho Desk organization");
    expect(markup).toContain('value="2389290"');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button">Save');
  });

  it("keeps Save disabled while orgId is blank, because the application refuses to start without it", () => {
    const markup = renderToStaticMarkup(<OrganizationPickerUnit canListOrganizations loadError="Zoho Desk returned no organization list. Enter the orgId below."
      onChooseOrganization={() => undefined} onSave={() => undefined} target={{...pickerTarget, value: {}}}/>);
    expect(markup).toContain("Zoho Desk returned no organization list.");
    expect(markup).not.toContain("numeric ID; a portal name");
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });
});
