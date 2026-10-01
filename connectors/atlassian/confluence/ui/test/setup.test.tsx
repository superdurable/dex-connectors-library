// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { ConfluenceSetupView } from "../src/setup.js";
import { ConfluenceConfigurationUnit, SitePickerUnit } from "../src/units.js";

const siteID = "1324a887-45db-1bf4-1e99-ef0ff456d421";

const spaceTarget: ConnectorStudioConfigurationUnitTarget = {
  kind: "configurationUnit", scope: {kind: "operation", operationId: "createPage", flowType: "ConfluencePolicyPublication", stepType: "PublishPolicyPage"},
  instanceId: "policySpace", unitId: "spacePicker", label: "Policy space",
  description: "Choose the Confluence space that holds the in-force policies. Leave it unsaved to use each Start Flow input's spaceKey.", required: false,
  bindings: [{port: "spaceId", jsonPointer: "/spaceId"}, {port: "spaceKey", jsonPointer: "/spaceKey"}, {port: "spaceName", jsonPointer: "/spaceName"}],
  value: {spaceId: "98306", spaceKey: "OPS", spaceName: "Operations"},
};

describe("Confluence setup", () => {
  it("shows the connected account without any credential", () => {
    const markup = renderToStaticMarkup(<ConfluenceSetupView connection={{state: "connected", accountEmail: "ada@example.com", grantedScopes: [
      "search:confluence", "read:page:confluence", "offline_access",
    ]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain("ada@example.com");
    expect(markup).toContain("Choose the Confluence site in the connection form.");
    expect(markup).not.toContain("access_token");
    expect(markup).not.toContain("refresh_token");
    expect(markup).not.toContain("client_secret");
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<ConfluenceSetupView connection={{state, grantedScopes: []}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("accept every requested scope");
    expect(markup).toContain("Reconnect Confluence");
  });

  it("renders the site picker with manual cloudId guidance and validation", () => {
    const markup = renderToStaticMarkup(<SitePickerUnit onChooseSite={() => undefined} onSave={() => undefined}
      sites={[{cloudId: siteID, name: "Ops", url: "https://ops.atlassian.net"}]}
      target={{kind: "connection", unitId: "sitePicker", bindings: [{port: "cloudId", jsonPointer: "/cloudId"}], value: {cloudId: "ops.atlassian.net"}}}/>);
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain("Leave it blank to use the only Confluence site the authorization grants.");
    expect(markup).toContain("Ops (https://ops.atlassian.net)");
    expect(markup).toContain("https://&lt;your-site&gt;.atlassian.net/_edge/tenant_info");
    expect(markup).toContain('role="alert">The cloudId must be a UUID; a site URL is not accepted.');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
    expect(markup).not.toContain("<style");
  });

  it("shows a site load failure inline", () => {
    const markup = renderToStaticMarkup(<SitePickerUnit loadError="Dex Web could not list Confluence sites. Enter the site's cloudId below."
      onChooseSite={() => undefined} onSave={() => undefined} target={{kind: "connection", unitId: "sitePicker", value: {}}}/>);
    expect(markup).toContain("Dex Web could not list Confluence sites.");
    expect(markup).toContain('class="studio-button studio-button-primary" type="button">Save');
  });

  it("renders the saved space with its Step-specific guidance", () => {
    const markup = renderToStaticMarkup(<ConfluenceConfigurationUnit connectionCloudId={siteID} onChooseSpace={() => undefined} onSave={() => undefined} target={spaceTarget}/>);
    expect(markup).toContain("Choose the Confluence space that holds the in-force policies. Leave it unsaved to use each Start Flow input&#x27;s spaceKey.");
    expect(markup).toContain("<strong>Space:</strong> Operations (OPS)");
    expect(markup).toContain('value="OPS"');
    expect(markup).toContain("Confluence &gt; Spaces &gt; the space &gt; Space settings");
    expect(markup).not.toContain("Confluence site cloudId");
    expect(markup).not.toContain("more spaces than one list can show");
  });

  it("asks for a listing site when the host reports no connection cloudId", () => {
    const markup = renderToStaticMarkup(<ConfluenceConfigurationUnit connectionCloudId="" isSpaceListTruncated
      onChooseSpace={() => undefined} onSave={() => undefined}
      spaces={[{id: "98306", key: "OPS", name: "Operations"}]} target={{...spaceTarget, value: {}}}/>);
    expect(markup).toContain("Confluence site cloudId");
    expect(markup).toContain("Used only to list spaces");
    expect(markup).toContain('class="studio-button" type="button" disabled="">Choose space');
    expect(markup).toContain("Operations (OPS)");
    expect(markup).toContain("Confluence returned more spaces than one list can show.");
  });

  it("rejects an invalid space key and requires a key for a required unit", () => {
    const invalid = renderToStaticMarkup(<ConfluenceConfigurationUnit connectionCloudId={siteID} onChooseSpace={() => undefined} onSave={() => undefined}
      target={{...spaceTarget, value: {spaceKey: "OPS SPACE"}}}/>);
    expect(invalid).toContain("The space key must be letters, digits, underscores, or hyphens");
    const required = renderToStaticMarkup(<ConfluenceConfigurationUnit connectionCloudId={siteID} onChooseSpace={() => undefined} onSave={() => undefined}
      target={{...spaceTarget, required: true, value: {}}}/>);
    expect(required).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });
});
