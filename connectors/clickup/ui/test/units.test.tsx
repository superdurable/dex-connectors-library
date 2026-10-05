// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { selectedSupportedEvents } from "../src/events.js";
import { ClickUpSetupView } from "../src/setup.js";
import { ClickUpConfigurationUnit } from "../src/units.js";

const triggerScope = {kind: "trigger" as const, triggerName: "taskEvent", bindingName: "blocked-status", flowType: "ClickUpEscalateBlockedTask"};

function eventTarget(value: Record<string, unknown>, required = false): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit", scope: triggerScope, instanceId: "events", unitId: "taskEventPicker", label: "Events that start the Flow",
    description: "Select taskStatusUpdated, the event ClickUp sends when a task's status changes.", required,
    bindings: [{port: "events", jsonPointer: "/events"}], value,
  };
}

function render(target: ConnectorStudioConfigurationUnitTarget) {
  return renderToStaticMarkup(<ClickUpConfigurationUnit onSave={() => undefined} target={target}/>);
}

describe("ClickUp configuration units", () => {
  it("renders every supported task event with its description and the saved selection", () => {
    const markup = render(eventTarget({events: ["taskStatusUpdated", "listCreated"]}));
    expect(markup).toContain("Events that start the Flow");
    expect(markup).toContain("Select taskStatusUpdated, the event ClickUp sends when a task&#x27;s status changes.");
    expect(markup.match(/type="checkbox"/g)).toHaveLength(13);
    expect(markup).toContain("taskCreated");
    expect(markup).toContain("taskTimeTrackedUpdated");
    expect(markup.match(/checked=""/g)).toHaveLength(1);
    expect(markup).toContain("Select none to accept every task event listed.");
    expect(markup).toContain("POST /api/v2/team/{team_id}/webhook");
    expect(markup).not.toContain("disabled");
  });

  it("blocks saving a required picker without an event", () => {
    expect(render(eventTarget({}, true))).toContain('disabled=""');
  });

  it("keeps only supported events in their supported order", () => {
    expect(selectedSupportedEvents(["taskMoved", "listCreated", "taskCreated", 3])).toEqual(["taskCreated", "taskMoved"]);
    expect(selectedSupportedEvents(undefined)).toEqual([]);
  });

  it("reports an unknown unit instead of rendering a guess", () => {
    expect(render({...eventTarget({}), unitId: "listPicker"})).toContain("Unsupported ClickUp configuration unit: listPicker");
  });
});

describe("ClickUp connection surface", () => {
  it("points credentials to the host form and never renders a credential field", () => {
    const markup = renderToStaticMarkup(<ClickUpSetupView connection={{state: "connected", grantedScopes: []}}/>);
    expect(markup).toContain("personal API token");
    expect(markup).toContain("Connected.");
    expect(markup).not.toContain("<input");
    expect(renderToStaticMarkup(<ClickUpSetupView connection={{state: "not_configured", grantedScopes: []}}/>)).toContain("Not connected yet.");
  });
});
