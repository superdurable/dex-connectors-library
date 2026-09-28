// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import type { ConnectorStudioClient } from "@superdurable/dex-connectors-react";

import { claudeModelsListCapability, loadClaudeModels, projectClaudeModelPage, readNextClaudeModelCursor } from "../src/provider.js";

const allCapabilities = {
  batch: {supported: true}, citations: {supported: true}, code_execution: {supported: true},
  effort: {supported: true, low: {supported: true}, medium: {supported: true}, high: {supported: true}, xhigh: {supported: true}, max: {supported: true}},
  image_input: {supported: true}, pdf_input: {supported: true}, structured_outputs: {supported: true},
  thinking: {supported: true, types: {adaptive: {supported: true}, enabled: {supported: false}}},
};

describe("projectClaudeModelPage", () => {
  it("labels models by display name, describes their limits, and badges their capabilities", () => {
    const models = projectClaudeModelPage({data: [
      {type: "model", id: "claude-sonnet-5", display_name: "Claude Sonnet 5", created_at: "2026-06-30T00:00:00Z",
        max_input_tokens: 1_000_000, max_tokens: 128_000, capabilities: allCapabilities},
      {type: "model", id: "claude-haiku-4-5-20251001", display_name: "Claude Haiku 4.5", created_at: "2025-10-01T00:00:00Z",
        max_input_tokens: 200_000, max_tokens: 64_000, capabilities: {...allCapabilities, effort: {supported: false}}},
      {type: "model", id: "claude-future-model", display_name: " ", max_input_tokens: 0, max_tokens: null, capabilities: null},
      {type: "model", id: "claude-sonnet-5", display_name: "Duplicate"},
    ], has_more: false, first_id: "claude-sonnet-5", last_id: "claude-future-model"});
    expect(models).toEqual([
      {id: "claude-sonnet-5", label: "Claude Sonnet 5", detail: "1M context · 128K max output",
        badges: ["structured output", "effort", "thinking"], isHiddenByDefault: false},
      {id: "claude-haiku-4-5-20251001", label: "Claude Haiku 4.5", detail: "200K context · 64K max output",
        badges: ["structured output", "thinking"], isHiddenByDefault: false},
      {id: "claude-future-model", label: undefined, detail: undefined, badges: [], isHiddenByDefault: false},
    ]);
  });

  it("returns no models for an unexpected shape", () => {
    expect(projectClaudeModelPage({models: []})).toEqual([]);
  });
});

describe("readNextClaudeModelCursor", () => {
  it("follows last_id only while has_more is true", () => {
    expect(readNextClaudeModelCursor({has_more: true, last_id: "claude-haiku-4-5"})).toBe("claude-haiku-4-5");
    expect(readNextClaudeModelCursor({has_more: false, last_id: "claude-haiku-4-5"})).toBe("");
    expect(readNextClaudeModelCursor({has_more: true, last_id: null})).toBe("");
  });
});

describe("loadClaudeModels", () => {
  it("pages with afterId and keeps Claude's order without duplicates", async () => {
    const calls: Array<{commandId: string; capability: string; parameters: Record<string, string> | undefined}> = [];
    const pages: Record<string, Record<string, unknown>> = {
      "": {data: [{id: "claude-opus-5-5", display_name: "Claude Opus 5.5"}, {id: "claude-sonnet-5"}], has_more: true, last_id: "claude-sonnet-5"},
      "claude-sonnet-5": {data: [{id: "claude-sonnet-5"}, {id: "claude-haiku-4-5"}], has_more: false, last_id: "claude-haiku-4-5"},
    };
    const client = {
      ready: undefined, busy: false,
      send: async () => ({}),
      executeProviderCommand: async (commandId: string, capability: string, parameters?: Record<string, string>) => {
        calls.push({commandId, capability, parameters});
        return pages[parameters?.afterId ?? ""];
      },
    } satisfies ConnectorStudioClient;
    const listing = await loadClaudeModels(client);
    expect(listing.models.map((model) => model.id)).toEqual(["claude-opus-5-5", "claude-sonnet-5", "claude-haiku-4-5"]);
    expect(listing.isTruncated).toBe(false);
    expect(calls).toEqual([
      {commandId: "listModels", capability: claudeModelsListCapability, parameters: {}},
      {commandId: "listModels", capability: claudeModelsListCapability, parameters: {afterId: "claude-sonnet-5"}},
    ]);
  });

  it("rejects when the host rejects the command, so the picker offers manual entry", async () => {
    const client = {
      ready: undefined, busy: false,
      send: async () => ({}),
      executeProviderCommand: async () => { throw new Error("anthropic-version: header is required"); },
    } satisfies ConnectorStudioClient;
    await expect(loadClaudeModels(client)).rejects.toThrow("anthropic-version");
  });
});
