// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
// @vitest-environment happy-dom

import { act, type ReactElement } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, describe, expect, it } from "vitest";

import {
  ModelPicker,
  ModelPickerStudioApp,
  connectorStudioHostAPIVersion,
  validateModelIDForRule,
  type ConnectorStudioConfigurationUnitTarget,
  type ConnectorStudioHostReady,
  type ModelListing,
} from "../src/index.js";

(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true;

const target = (value: Record<string, unknown> = {}): ConnectorStudioConfigurationUnitTarget => ({
  kind: "configurationUnit",
  scope: {kind: "operation", operationId: "generateText", flowType: "Summarize", stepType: "GenerateSummary"},
  instanceId: "model", unitId: "modelPicker", label: "Summary model", required: false,
  bindings: [{port: "model", jsonPointer: "/model"}], value,
});

const listing: ModelListing = {models: [{id: "openai/gpt-6-sol", label: "GPT-6 Sol"}, {id: "gemini/gemini-3.5-flash-lite"}]};

/** validateGeminiQualifiedModel accepts gemini/<path-segment ID>, as a routing connector's bundle would. */
function validateGeminiQualifiedModel(model: string): string | undefined {
  if (!model.startsWith("gemini/")) return "Enter gemini/<model-id>.";
  const validation = validateModelIDForRule("pathSegment", model.slice("gemini/".length));
  return validation.isValid ? undefined : validation.message;
}

const mountedContainers: {unmount(): void}[] = [];

afterEach(() => {
  for (const container of mountedContainers.splice(0)) container.unmount();
  document.getElementById("dex-connector-studio-theme")?.remove();
});

async function renderInDocument(element: ReactElement, afterRender?: () => void): Promise<HTMLElement> {
  const container = document.createElement("div");
  document.body.append(container);
  const reactRoot = createRoot(container);
  mountedContainers.push({unmount: () => { act(() => reactRoot.unmount()); container.remove(); }});
  await act(async () => { reactRoot.render(element); });
  if (afterRender) await act(async () => { afterRender(); });
  return container;
}

function manualEntry(container: HTMLElement): HTMLInputElement {
  const input = container.querySelector<HTMLInputElement>('input[type="text"]');
  if (!input) throw new Error("manual model entry is missing");
  return input;
}

function saveButton(container: HTMLElement): HTMLButtonElement {
  const button = [...container.querySelectorAll<HTMLButtonElement>("button")].find((candidate) => candidate.textContent === "Save");
  if (!button) throw new Error("Save button is missing");
  return button;
}

function retryButton(container: HTMLElement): HTMLButtonElement {
  const button = [...container.querySelectorAll<HTMLButtonElement>("button")].find((candidate) => candidate.textContent === "Retry");
  if (!button) throw new Error("Retry button is missing");
  return button;
}

function modelRadio(container: HTMLElement, model: string): HTMLInputElement {
  const radio = container.querySelector<HTMLInputElement>(`input[type="radio"][value="${model}"]`);
  if (!radio) throw new Error(`model option ${JSON.stringify(model)} is missing`);
  return radio;
}

function selectedModelText(container: HTMLElement): string | null | undefined {
  return container.querySelector(".studio-actions .studio-muted")?.textContent;
}

function noticeTexts(container: HTMLElement): string[] {
  return [...container.querySelectorAll(".studio-notice")].map((notice) => `${notice.className}: ${notice.textContent}`);
}

async function typeManualModel(container: HTMLElement, value: string): Promise<void> {
  const input = manualEntry(container);
  const setValue = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(input), "value")?.set;
  await act(async () => {
    setValue?.call(input, value);
    input.dispatchEvent(new Event("input", {bubbles: true}));
  });
}

describe("ModelPicker listing notices", () => {
  it("shows each notice in order with its tone once the list loads", async () => {
    const container = await renderInDocument(<ModelPicker
      loadModels={async () => ({...listing, notices: [
        {tone: "attention", message: "Claude models could not be listed."},
        {tone: "info", message: "Gemini lists through its OpenAI-compatible endpoint."},
      ]})}
      onSave={async () => ({})} providerName="LLM" target={target()}/>);
    expect(noticeTexts(container)).toEqual([
      "studio-notice studio-notice-attention: Claude models could not be listed.",
      "studio-notice studio-notice-info: Gemini lists through its OpenAI-compatible endpoint.",
    ]);
    expect([...container.querySelectorAll(".studio-notice")].map((notice) => notice.getAttribute("role"))).toEqual(["status", "status"]);
  });

  it("renders a listing without notices as before", async () => {
    const container = await renderInDocument(<ModelPicker loadModels={async () => listing} onSave={async () => ({})} providerName="LLM" target={target()}/>);
    expect(noticeTexts(container)).toEqual([]);
  });
});

describe("ModelPicker manual model validation", () => {
  it("disables Save and shows the message while a typed model is invalid", async () => {
    const saved: string[] = [];
    const container = await renderInDocument(<ModelPicker
      loadModels={async () => listing} onSave={async (value) => { saved.push(value.model); }}
      providerName="LLM" target={target()} validateManualModel={validateGeminiQualifiedModel}/>);
    expect(saveButton(container).disabled).toBe(false);
    expect(manualEntry(container).hasAttribute("aria-invalid")).toBe(false);

    await typeManualModel(container, "gemini/models:bad");
    expect(saveButton(container).disabled).toBe(true);
    expect(manualEntry(container).getAttribute("aria-invalid")).toBe("true");
    expect(noticeTexts(container)).toEqual([
      'studio-notice studio-notice-attention: The model ID must start with a letter or digit and contain at most 128 letters, digits, ".", "_", or "-".',
    ]);

    await typeManualModel(container, "  gemini/gemini-4-pro  ");
    expect(saveButton(container).disabled).toBe(false);
    expect(manualEntry(container).hasAttribute("aria-invalid")).toBe(false);
    expect(noticeTexts(container)).toEqual([]);
    await act(async () => { saveButton(container).click(); });
    expect(saved).toEqual(["gemini/gemini-4-pro"]);
  });

  it("checks neither a listed model, the connection default, nor a blank entry", async () => {
    const checked: string[] = [];
    const saved: string[] = [];
    const container = await renderInDocument(<ModelPicker
      loadModels={async () => listing} onSave={async (value) => { saved.push(value.model); }} providerName="LLM" target={target()}
      validateManualModel={(model) => { checked.push(model); return "Not accepted."; }}/>);
    await typeManualModel(container, "anything");
    expect(saveButton(container).disabled).toBe(true);
    await act(async () => { modelRadio(container, "openai/gpt-6-sol").click(); });
    expect(manualEntry(container).value).toBe("");
    expect(saveButton(container).disabled).toBe(false);
    await typeManualModel(container, "   ");
    expect(saveButton(container).disabled).toBe(false);
    await typeManualModel(container, "again");
    expect(saveButton(container).disabled).toBe(true);
    await act(async () => { modelRadio(container, "").click(); });
    expect(manualEntry(container).value).toBe("");
    expect(saveButton(container).disabled).toBe(false);
    await act(async () => { saveButton(container).click(); });
    expect(saved).toEqual([""]);
    expect(new Set(checked)).toEqual(new Set(["anything", "again"]));
  });

  it("keeps a model typed while the list loads instead of the saved model", async () => {
    const saved: string[] = [];
    let resolveListing: (value: ModelListing) => void = () => {};
    const container = await renderInDocument(<ModelPicker
      loadModels={() => new Promise<ModelListing>((resolve) => { resolveListing = resolve; })}
      onSave={async (value) => { saved.push(value.model); }} providerName="LLM" target={target({model: "gemini/saved-model"})}
      validateManualModel={validateGeminiQualifiedModel}/>);
    await typeManualModel(container, "not valid!!");
    expect(saveButton(container).disabled).toBe(true);

    await act(async () => { resolveListing(listing); });
    expect(manualEntry(container).value).toBe("not valid!!");
    expect(selectedModelText(container)).toBe("Selected: not valid!!");
    expect(saveButton(container).disabled).toBe(true);
    await act(async () => { saveButton(container).click(); });
    expect(saved).toEqual([]);
  });

  it("keeps a typed model and a chosen default across Retry", async () => {
    for (const retryListing of [async (): Promise<ModelListing> => { throw new Error("still offline"); }, async () => listing]) {
      for (const choose of [
        (container: HTMLElement) => typeManualModel(container, "not valid!!"),
        (container: HTMLElement) => act(async () => { modelRadio(container, "").click(); }),
      ]) {
        const saved: string[] = [];
        let loadCount = 0;
        const container = await renderInDocument(<ModelPicker
          loadModels={async () => { loadCount += 1; if (loadCount === 1) throw new Error("offline"); return retryListing(); }}
          onSave={async (value) => { saved.push(value.model); }} providerName="LLM" target={target({model: "gemini/saved-model"})}
          validateManualModel={validateGeminiQualifiedModel}/>);
        expect(manualEntry(container).value).toBe("gemini/saved-model");
        await choose(container);
        const chosenEntry = manualEntry(container).value;
        const chosenSelection = selectedModelText(container);
        const isSaveDisabled = saveButton(container).disabled;

        await act(async () => { retryButton(container).click(); });
        expect(loadCount).toBe(2);
        expect(manualEntry(container).value).toBe(chosenEntry);
        expect(selectedModelText(container)).toBe(chosenSelection);
        expect(saveButton(container).disabled).toBe(isSaveDisabled);
        await act(async () => { saveButton(container).click(); });
        expect(saved).toEqual(isSaveDisabled ? [] : [""]);
      }
    }
  });

  it("flags a saved model the list lacks and the manual entry after a failed list", async () => {
    for (const loadModels of [async () => listing, async (): Promise<ModelListing> => { throw new Error("offline"); }]) {
      const container = await renderInDocument(<ModelPicker
        loadModels={loadModels} onSave={async () => ({})} providerName="LLM" target={target({model: "claude-sonnet-5"})}
        validateManualModel={validateGeminiQualifiedModel}/>);
      expect(manualEntry(container).value).toBe("claude-sonnet-5");
      expect(saveButton(container).disabled).toBe(true);
      expect(noticeTexts(container)).toContain("studio-notice studio-notice-attention: Enter gemini/<model-id>.");
    }
  });

  it("accepts every typed model without a validator, as before", async () => {
    const container = await renderInDocument(<ModelPicker loadModels={async () => listing} onSave={async () => ({})} providerName="LLM" target={target()}/>);
    await typeManualModel(container, "gemini/models:bad");
    expect(saveButton(container).disabled).toBe(false);
    expect(manualEntry(container).hasAttribute("aria-invalid")).toBe(false);
    expect(manualEntry(container).placeholder).toBe("model-id");
  });
});

describe("ModelPickerStudioApp picker options", () => {
  it("passes the manual model validator and placeholder to the picker", async () => {
    const ready: ConnectorStudioHostReady = {
      type: "connector.host.ready", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: "nonce", connectorId: "llm",
      capabilities: [], connection: {state: "connected", grantedScopes: []}, target: target(),
    };
    const container = await renderInDocument(
      <ModelPickerStudioApp connectorId="llm" loadModels={async () => listing} manualModelPlaceholder="provider/model-id"
        providerName="LLM" validateManualModel={validateGeminiQualifiedModel}/>,
      () => window.dispatchEvent(new MessageEvent("message", {data: ready, source: window.parent})));
    expect(manualEntry(container).placeholder).toBe("provider/model-id");
    await typeManualModel(container, "openai/gpt-6-sol");
    expect(saveButton(container).disabled).toBe(true);
    expect(noticeTexts(container)).toEqual(["studio-notice studio-notice-attention: Enter gemini/<model-id>."]);
  });
});
