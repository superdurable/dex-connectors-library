// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { boardIDFromEntry, savedBoardID } from "../src/board.js";
import { MondaySetupView } from "../src/setup.js";
import { MondayConfigurationUnit } from "../src/units.js";

const triggerScope = {kind: "trigger" as const, triggerName: "itemChanged", bindingName: "board-item-changes", flowType: "MondayItemChange"};

function boardTarget(value: Record<string, unknown>): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit", scope: triggerScope, instanceId: "watchedBoard", unitId: "boardIdInput", label: "Watched board",
    description: "Enter the board whose created and changed items start this Flow.", required: true,
    bindings: [{port: "boardId", jsonPointer: "/boardId"}], value,
  };
}

function render(target: ConnectorStudioConfigurationUnitTarget) {
  return renderToStaticMarkup(<MondayConfigurationUnit onSave={() => undefined} target={target}/>);
}

describe("monday.com configuration units", () => {
  it("renders the only visible field with its guidance and the saved board ID", () => {
    const markup = render(boardTarget({boardId: "1234567890"}));
    expect(markup).toContain("Watched board");
    expect(markup).toContain("Enter the board whose created and changed items start this Flow.");
    expect(markup.match(/<input/g)).toHaveLength(1);
    expect(markup).toContain('value="1234567890"');
    expect(markup).toContain("https://monday.com/");
    expect(markup).toContain("digits after /boards/");
    expect(markup).not.toContain("disabled");
  });

  it("explains blank and blocks saving it", () => {
    const markup = render(boardTarget({}));
    expect(markup).toContain("Blank saves nothing");
    expect(markup).toContain('disabled=""');
  });

  it("ignores a malformed saved value", () => {
    expect(render(boardTarget({boardId: "board-1"}))).toContain('value=""');
  });

  it("accepts a numeric ID or a board address and rejects anything else", () => {
    expect(boardIDFromEntry(" 1234567890 ")).toBe("1234567890");
    expect(boardIDFromEntry("https://contoso.monday.com/boards/1234567890/views/55?term=x")).toBe("1234567890");
    expect(boardIDFromEntry("https://evil.example/boards/1234567890")).toBe("");
    expect(boardIDFromEntry("0123")).toBe("");
    expect(boardIDFromEntry("12a")).toBe("");
    expect(savedBoardID(1234567890)).toBe("");
  });

  it("reports an unknown unit instead of rendering a guess", () => {
    expect(render({...boardTarget({}), unitId: "groupPicker"})).toContain("Unsupported monday.com configuration unit: groupPicker");
  });
});

describe("monday.com connection surface", () => {
  it("points credentials to the host form and never renders a credential field", () => {
    const markup = renderToStaticMarkup(<MondaySetupView connection={{state: "connected", grantedScopes: []}}/>);
    expect(markup).toContain("personal API token");
    expect(markup).toContain("Connected.");
    expect(markup).not.toContain("<input");
    expect(renderToStaticMarkup(<MondaySetupView connection={{state: "not_configured", grantedScopes: []}}/>)).toContain("Not connected yet.");
  });
});
