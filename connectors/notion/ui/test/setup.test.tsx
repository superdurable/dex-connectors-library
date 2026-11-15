// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { NotionSetupView } from "../src/setup.js";
import { NotionConfigurationUnit } from "../src/units.js";

const sentinelToken = "ntn_SENTINEL_NOTION_API_TOKEN";
const scope = {kind: "trigger" as const, triggerName: "databaseRowChanged", bindingName: "database-row-changes", flowType: "NotionDatabaseRowChange"};
const bindings = [{port: "dataSourceId", jsonPointer: "/dataSourceId"}, {port: "dataSourceName", jsonPointer: "/dataSourceName"}];

describe("Notion setup", () => {
  it("keeps the token in the host form and explains each connection state", () => {
    const view = (state: "not_configured" | "connected" | "revoked") =>
      renderToStaticMarkup(<NotionSetupView connection={{state, grantedScopes: [], authMethodIds: ["api-key"]}}/>);
    const notConfigured = view("not_configured");
    expect(notConfigured).toContain("https://app.notion.com/developers/connections");
    expect(notConfigured).toContain("never sends it to this panel");
    expect(view("connected")).toContain("resolve live from Notion");
    expect(view("revoked")).toContain("choose Refresh on its Configuration tab");
    for (const markup of [notConfigured, view("connected"), view("revoked")]) expect(markup).not.toContain(sentinelToken);
  });

  it("renders the data source picker with its guidance, listed sources, and the saved value", () => {
    const description = "Choose the Notion data source whose created and edited rows start this Flow.";
    const markup = renderToStaticMarkup(<NotionConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "watchedDataSource", unitId: "dataSourcePicker", label: "Watched database",
        description, required: false, bindings, value: {dataSourceId: "2a4d6f80-1b3c-4d5e-8f70-112233445566", dataSourceName: "Requests"}}}
      dataSources={[{id: "2a4d6f80-1b3c-4d5e-8f70-112233445566", name: "Requests"}]}
      onCheckDataSource={() => undefined} onLoadDataSources={() => undefined} onSave={() => undefined}/>);
    expect(markup).toContain(description);
    for (const text of ["Database link or ID", "••• menu &gt; Copy link", "shared with this connection", "Load data sources",
      "Requests · 2a4d6f80-1b3c-4d5e-8f70-112233445566", "Manage data sources &gt; Copy data source ID", "Check data source", "Saved: Requests"]) {
      expect(markup).toContain(text);
    }
    expect(markup).not.toContain(sentinelToken);
  });

  it("explains blank and invalid values, and rejects an unknown unit", () => {
    const render = (value: Record<string, unknown>, unitId = "dataSourcePicker") => renderToStaticMarkup(<NotionConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "x", unitId, label: "Watched database", description: "Pick one.", required: false, bindings, value}}
      dataSources={[]} onCheckDataSource={() => undefined} onLoadDataSources={() => undefined} onSave={() => undefined}/>);
    expect(render({})).toContain("the Trigger cannot start until one is");
    expect(render({})).toContain("Load a database first");
    expect(render({dataSourceId: "not-an-id"})).toContain("Enter a data source ID of 32 hexadecimal characters");
    expect(render({}, "textInput")).toContain("Unsupported Notion configuration unit: textInput");
  });
});
