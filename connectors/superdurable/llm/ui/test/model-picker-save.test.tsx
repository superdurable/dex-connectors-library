// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
// @vitest-environment happy-dom

import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, describe, expect, it } from "vitest";
import { ModelPicker, type ConnectorStudioClient, type ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";

import { loadLLMModels, validateProviderQualifiedModel } from "../src/model-selection.js";

(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true;

const target = (value: Record<string, unknown> = {}): ConnectorStudioConfigurationUnitTarget => ({
  kind: "configurationUnit",
  scope: {kind: "operation", operationId: "generateText", flowType: "LLMSummarizeText", stepType: "SummarizeText"},
  instanceId: "summaryModel", unitId: "modelPicker", label: "Summary model", required: false,
  bindings: [{port: "model", jsonPointer: "/model"}], value,
});

// These hosts predate the connection context, so they report no auth methods and every provider is listed.
const oldHostConnection = {authMethodIds: []};

// Only the OpenAI list answers, as on Dex Web cli-v0.13.8 with only an OpenAI key.
const openAIOnlyClient = {
  ready: undefined, busy: false,
  send: async () => ({}),
  executeProviderCommand: async (commandId: string) => {
    if (commandId !== "listOpenAIModels") throw new Error("Connector provider command failed");
    return {data: [{id: "gpt-6-sol", created: 1}]};
  },
} satisfies ConnectorStudioClient;

// Every list fails, as on Dex Web cli-v0.13.8 with only an Anthropic key.
const everyListFailsClient = {
  ready: undefined, busy: false,
  send: async () => ({}),
  executeProviderCommand: async () => { throw new Error("Connector provider command failed"); },
} satisfies ConnectorStudioClient;

const unmountCallbacks: Array<() => void> = [];

afterEach(() => {
  for (const unmount of unmountCallbacks.splice(0)) unmount();
});

async function renderPicker(
  value: Record<string, unknown>, saved: Array<{model: string}>, client: ConnectorStudioClient = openAIOnlyClient,
): Promise<HTMLElement> {
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  unmountCallbacks.push(() => { act(() => root.unmount()); container.remove(); });
  await act(async () => {
    root.render(<ModelPicker
      loadModels={() => loadLLMModels(client, oldHostConnection)}
      manualModelPlaceholder="provider/model-id"
      onSave={async (pick) => { saved.push(pick); }}
      providerName="LLM"
      target={target(value)}
      validateManualModel={validateProviderQualifiedModel}
    />);
  });
  return container;
}

function saveButton(container: HTMLElement): HTMLButtonElement {
  const button = Array.from(container.querySelectorAll<HTMLButtonElement>("button")).find((candidate) => candidate.textContent === "Save");
  if (!button) throw new Error("Save button is missing");
  return button;
}

async function typeModel(container: HTMLElement, model: string): Promise<void> {
  const input = container.querySelector<HTMLInputElement>('input[type="text"]');
  if (!input) throw new Error("manual model entry is missing");
  await act(async () => {
    const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
    setValue?.call(input, model);
    input.dispatchEvent(new Event("input", {bubbles: true}));
  });
}

describe("the llm model picker", () => {
  it("saves a provider's default model when its list fails", async () => {
    const saved: Array<{model: string}> = [];
    const container = await renderPicker({}, saved);
    expect(container.textContent).toContain("Claude models could not be listed.");
    const claudeDefault = container.querySelector<HTMLInputElement>('input[type="radio"][value="anthropic"]');
    expect(claudeDefault).not.toBeNull();
    await act(async () => { claudeDefault?.click(); });
    await act(async () => { saveButton(container).click(); });
    expect(saved).toEqual([{model: "anthropic"}]);
  });

  it("offers every provider's default model and manual entry when every list fails", async () => {
    const saved: Array<{model: string}> = [];
    const container = await renderPicker({}, saved, everyListFailsClient);
    const options = Array.from(container.querySelectorAll<HTMLInputElement>('input[type="radio"]'));
    expect(options.map((option) => option.value)).toEqual(["", "openai", "anthropic", "gemini"]);
    expect(options.map((option) => option.closest("label")?.querySelector(".studio-option-label")?.textContent)).toEqual([
      "Use the connection's default model", "OpenAI default model", "Claude default model", "Gemini default model",
    ]);
    expect(container.textContent).toContain("No provider's models could be listed.");
    expect(container.textContent).toContain("Claude models could not be listed.");
    expect(container.querySelector(".studio-notice-error"), "the picker does not fall into its failed state").toBeNull();
    expect(container.querySelector('input[type="text"]')).not.toBeNull();
    await act(async () => { container.querySelector<HTMLInputElement>('input[type="radio"][value="anthropic"]')?.click(); });
    await act(async () => { saveButton(container).click(); });
    expect(saved).toEqual([{model: "anthropic"}]);
  });

  it("saves a listed model as provider/model and shows its provider badge", async () => {
    const saved: Array<{model: string}> = [];
    const container = await renderPicker({}, saved);
    const listed = container.querySelector<HTMLInputElement>('input[type="radio"][value="openai/gpt-6-sol"]');
    expect(listed?.closest("label")?.querySelector(".studio-badge")?.textContent).toBe("OpenAI");
    await act(async () => { listed?.click(); });
    await act(async () => { saveButton(container).click(); });
    expect(saved).toEqual([{model: "openai/gpt-6-sol"}]);
  });

  it("accepts a typed provider/model and blocks a bare model ID", async () => {
    const saved: Array<{model: string}> = [];
    const container = await renderPicker({}, saved);
    await typeModel(container, "claude-opus-5-5");
    expect(saveButton(container).disabled).toBe(true);
    expect(container.textContent).toContain("Enter provider/model");
    await typeModel(container, " anthropic/claude-opus-5-5 ");
    expect(saveButton(container).disabled).toBe(false);
    await act(async () => { saveButton(container).click(); });
    expect(saved).toEqual([{model: "anthropic/claude-opus-5-5"}]);
  });

  it("shows a saved pick that the lists do not contain in the entry", async () => {
    const container = await renderPicker({model: "gemini/gemini-3.8-flash"}, []);
    expect(container.querySelector<HTMLInputElement>('input[type="text"]')?.value).toBe("gemini/gemini-3.8-flash");
    expect(saveButton(container).disabled).toBe(false);
  });
});
