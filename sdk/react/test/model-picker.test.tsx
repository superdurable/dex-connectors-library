// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import {
  ModelPicker,
  StudioButton,
  collectProviderPages,
  connectorStudioStyles,
  filterModelOptions,
  savedModel,
  type ConnectorStudioConfigurationUnitTarget,
  type ModelOption,
} from "../src/index.js";

const target = (value: Record<string, unknown> = {}): ConnectorStudioConfigurationUnitTarget => ({
  kind: "configurationUnit",
  scope: {kind: "operation", operationId: "generateContent", flowType: "Summarize", stepType: "GenerateSummary"},
  instanceId: "model", unitId: "modelPicker", label: "Summary model", required: false,
  bindings: [{port: "model", jsonPointer: "/model"}], value,
});

const models: ModelOption[] = [
  {id: "gemini-3.8-flash", label: "Gemini 3.8 Flash", badges: ["structured output"]},
  {id: "gemini-3.5-flash-lite", label: "Gemini 3.5 Flash-Lite"},
  {id: "text-embedding-005", label: "Text Embedding", isHiddenByDefault: true},
];

describe("filterModelOptions", () => {
  it("hides filtered-out models unless all are shown, keeping provider order", () => {
    expect(filterModelOptions(models, "", false).map((model) => model.id)).toEqual(["gemini-3.8-flash", "gemini-3.5-flash-lite"]);
    expect(filterModelOptions(models, "", true)).toHaveLength(3);
  });

  it("matches the ID or the label without case", () => {
    expect(filterModelOptions(models, "LITE", false).map((model) => model.id)).toEqual(["gemini-3.5-flash-lite"]);
    expect(filterModelOptions(models, "3.8", false).map((model) => model.id)).toEqual(["gemini-3.8-flash"]);
    expect(filterModelOptions(models, "embedding", false)).toEqual([]);
  });
});

describe("savedModel", () => {
  it("reads a trimmed saved model and treats anything else as the connection default", () => {
    expect(savedModel(target({model: " gemini-3.8-flash "}))).toBe("gemini-3.8-flash");
    expect(savedModel(target({model: ""}))).toBe("");
    expect(savedModel(target({model: null}))).toBe("");
    expect(savedModel(target())).toBe("");
  });
});

describe("collectProviderPages", () => {
  it("follows cursors until the provider returns an empty one", async () => {
    const pages: Record<string, {items: number[]; nextCursor: string}> = {"": {items: [1, 2], nextCursor: "b"}, b: {items: [3], nextCursor: ""}};
    const requested: string[] = [];
    const result = await collectProviderPages(async (cursor) => { requested.push(cursor); return pages[cursor]; });
    expect(result).toEqual({items: [1, 2, 3], isTruncated: false});
    expect(requested).toEqual(["", "b"]);
  });

  it("stops on a repeated cursor instead of looping", async () => {
    const result = await collectProviderPages(async () => ({items: [1], nextCursor: "same"}));
    expect(result).toEqual({items: [1, 1], isTruncated: false});
  });

  it("reports truncation when maxPages stops paging", async () => {
    let page = 0;
    const result = await collectProviderPages(async () => ({items: [page], nextCursor: `c${++page}`}), 3);
    expect(result).toEqual({items: [0, 1, 2], isTruncated: true});
  });
});

describe("ModelPicker", () => {
  it("renders the loading state, the connection default, and manual entry", () => {
    const markup = renderToStaticMarkup(<ModelPicker
      loadModels={async () => ({models})} onSave={async () => ({})} providerName="Gemini" target={target()}/>);
    expect(markup).toContain("Loading models from Gemini");
    expect(markup).toContain("Use the connection&#x27;s default model");
    expect(markup).toContain("Or enter a model ID");
    expect(markup).toContain("studio-surface");
    expect(markup).not.toMatch(/api[_ ]?key|token|secret/i);
  });
});

describe("shared Studio styling", () => {
  it("uses Dex Web's CTA green in both themes", () => {
    expect(connectorStudioStyles).toContain("--studio-cta: #008650");
    expect(connectorStudioStyles).toContain(":root[data-theme='dark']");
    expect(connectorStudioStyles).toContain("--studio-cta: #70eea9");
  });

  it("renders buttons as non-submitting by default", () => {
    expect(renderToStaticMarkup(<StudioButton variant="primary">Save</StudioButton>))
      .toBe('<button class="studio-button studio-button-primary" type="button">Save</button>');
  });
});
