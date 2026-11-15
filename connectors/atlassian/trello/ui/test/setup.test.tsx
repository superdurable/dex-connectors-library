// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { boardIdFromInput } from "../src/board.js";
import { TrelloSetupView } from "../src/setup.js";
import { TrelloConfigurationUnit } from "../src/units.js";

const sentinelToken = "SENTINEL-TRELLO-TOKEN";
const scope = {kind: "trigger" as const, triggerName: "cardChanged", bindingName: "board-card-changes", flowType: "TrelloCardChange"};

function boardInput(value: Record<string, unknown>) {
  return renderToStaticMarkup(<TrelloConfigurationUnit
    target={{kind: "configurationUnit", scope, instanceId: "watchedBoard", unitId: "boardInput", label: "Watched board",
      description: "Paste the board whose card actions start this Flow.", required: true,
      bindings: [{port: "boardId", jsonPointer: "/boardId"}], value}}
    onSave={() => undefined}/>);
}

describe("Trello setup", () => {
  it("keeps the key and token in the host form and explains each connection state", () => {
    const notConfigured = renderToStaticMarkup(<TrelloSetupView connection={{state: "not_configured", grantedScopes: []}}/>);
    expect(notConfigured).toContain("https://trello.com/apps/admin");
    expect(notConfigured).toContain("never sends them to this panel");
    const connected = renderToStaticMarkup(<TrelloSetupView connection={{state: "connected", grantedScopes: []}}/>);
    expect(connected).toContain("every board that member can open");
    const revoked = renderToStaticMarkup(<TrelloSetupView connection={{state: "revoked", grantedScopes: []}}/>);
    expect(revoked).toContain("https://trello.com/u/your-username/account");
    for (const markup of [notConfigured, connected, revoked]) expect(markup).not.toContain(sentinelToken);
  });

  it("derives the saved board from a board link, a short link, or a board ID", () => {
    expect(boardIdFromInput(" https://trello.com/b/AbCd1234/support-queue ")).toBe("AbCd1234");
    expect(boardIdFromInput("https://trello.com/b/AbCd1234")).toBe("AbCd1234");
    expect(boardIdFromInput("AbCd1234")).toBe("AbCd1234");
    expect(boardIdFromInput("5b6893f01cb3228998cf629e")).toBe("5b6893f01cb3228998cf629e");
    for (const invalid of ["", "https://trello.com/c/AbCd1234/card", "http://trello.com/b/AbCd1234/x", "AbCd123", "support queue"]) {
      expect(boardIdFromInput(invalid)).toBeUndefined();
    }
  });

  it("renders the board input with its guidance, derived value, and disabled Save until valid", () => {
    const saved = boardInput({boardId: "AbCd1234"});
    expect(saved).toContain("Watched board");
    expect(saved).toContain("Paste the board whose card actions start this Flow.");
    expect(saved).toContain("https://trello.com/b/&lt;short link&gt;/&lt;board name&gt;");
    expect(saved).toContain('placeholder="https://trello.com/b/AbCd1234/support-queue"');
    expect(saved).toContain("Saves board <strong>AbCd1234</strong>");
    expect(saved).toContain('class="studio-surface"');
    const blank = boardInput({});
    expect(blank).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
    const card = boardInput({boardId: "https://trello.com/c/AbCd1234/card"});
    expect(card).toContain("a card link does not identify a board");
  });

  it("rejects an unknown unit", () => {
    const unknown = renderToStaticMarkup(<TrelloConfigurationUnit
      target={{kind: "configurationUnit", scope, instanceId: "x", unitId: "listPicker", label: "X", required: false, bindings: [], value: {}}}
      onSave={() => undefined}/>);
    expect(unknown).toContain("Unsupported Trello configuration unit: listPicker");
  });
});
