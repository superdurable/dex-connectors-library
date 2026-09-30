// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
// @vitest-environment happy-dom

import { act } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  ModelPickerStudioApp,
  connectorStudioHostAPIVersion,
  type ConnectorConnectionView,
  type ConnectorStudioCommand,
  type ConnectorStudioHostReady,
  type ConnectorStudioTarget,
} from "@superdurable/dex-connectors-react";

import { llmModelPickerBundleConfig } from "../src/bundle-config.js";

(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true;

const summaryModelTarget: ConnectorStudioTarget = {
  kind: "configurationUnit",
  scope: {kind: "operation", operationId: "generateText", flowType: "LLMSummarizeText", stepType: "SummarizeText"},
  instanceId: "summaryModel", unitId: "modelPicker", label: "Summary model", required: false,
  bindings: [{port: "model", jsonPointer: "/model"}], value: {},
};

// The host renders the modelPicker unit for the connection's model field, as the manifest's studioUnit declares.
const connectionModelTarget = (value: Record<string, unknown> = {}): ConnectorStudioTarget => ({
  kind: "connection", unitId: "modelPicker", bindings: [{port: "model", jsonPointer: "/model"}], value,
});

const connection = (authMethodIds: string[], configuration: Record<string, unknown>): ConnectorConnectionView => ({
  state: "connected", grantedScopes: [], authMethodIds, configuration,
});

const hostReady = (connectionView: ConnectorConnectionView, target: ConnectorStudioTarget): ConnectorStudioHostReady => ({
  type: "connector.host.ready", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: "llm-bundle-nonce", connectorId: "llm",
  capabilities: ["use.configuration.write", "llm.models-list"], connection: connectionView, target,
});

const providerLists: Record<string, Record<string, unknown>> = {
  listOpenAIModels: {data: [{id: "gpt-6-sol", created: 1}]},
  listAnthropicModels: {data: [{id: "claude-sonnet-5", display_name: "Claude Sonnet 5"}], has_more: false},
  listGeminiModels: {models: [{name: "models/gemini-3.8-flash", supportedGenerationMethods: ["generateContent"]}]},
};

const unmountCallbacks: Array<() => void> = [];

afterEach(() => {
  for (const unmount of unmountCallbacks.splice(0)) unmount();
  document.getElementById("dex-connector-studio-theme")?.remove();
  vi.restoreAllMocks();
});

function sendFromHost(message: unknown): void {
  window.dispatchEvent(new MessageEvent("message", {data: message, source: window.parent}));
}

/** answerHostCommands records each command the bundle posts and answers it as a host does: provider lists by command ID, saves with {}. */
function answerHostCommands(): ConnectorStudioCommand[] {
  const commands: ConnectorStudioCommand[] = [];
  vi.spyOn(window.parent, "postMessage").mockImplementation(((message: {type?: unknown}) => {
    if (message.type !== "connector.command") return;
    const command = message as ConnectorStudioCommand;
    commands.push(command);
    const commandId = String(command.input?.commandId ?? "");
    const value = command.command === "provider.command.execute" ? providerLists[commandId] : {};
    queueMicrotask(() => sendFromHost({
      type: "connector.command.result", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: command.sessionNonce,
      connectorId: command.connectorId, requestId: command.requestId,
      ...(value ? {ok: true, value} : {ok: false, error: {code: "PROVIDER_REJECTED", message: "Connector provider command failed"}}),
    }));
  }) as typeof window.postMessage);
  return commands;
}

async function renderBundle(ready: ConnectorStudioHostReady): Promise<HTMLElement> {
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  unmountCallbacks.push(() => { act(() => root.unmount()); container.remove(); });
  await act(async () => { root.render(<ModelPickerStudioApp {...llmModelPickerBundleConfig}/>); });
  await act(async () => { sendFromHost(ready); });
  return container;
}

function optionValues(container: HTMLElement): string[] {
  return Array.from(container.querySelectorAll<HTMLInputElement>('input[type="radio"]')).map((radio) => radio.value);
}

async function waitForOptions(container: HTMLElement, expected: string[]): Promise<void> {
  await vi.waitFor(async () => {
    await act(async () => {});
    expect(optionValues(container)).toEqual(expected);
  });
}

function firstOptionLabel(container: HTMLElement): string | null | undefined {
  return container.querySelector(".studio-option .studio-option-label")?.textContent;
}

function saveButton(container: HTMLElement): HTMLButtonElement {
  const button = Array.from(container.querySelectorAll<HTMLButtonElement>("button")).find((candidate) => candidate.textContent === "Save");
  if (!button) throw new Error("Save button is missing");
  return button;
}

function providerCommandIDs(commands: ConnectorStudioCommand[]): string[] {
  return commands.filter((command) => command.command === "provider.command.execute").map((command) => String(command.input?.commandId));
}

describe("the llm Studio bundle on a Step", () => {
  it("lists only the added providers and names the connection's model in the first option", async () => {
    const commands = answerHostCommands();
    const container = await renderBundle(hostReady(connection(["gemini", "anthropic"], {model: "anthropic/claude-sonnet-5"}), summaryModelTarget));
    await waitForOptions(container, ["", "anthropic", "gemini", "anthropic/claude-sonnet-5", "gemini/gemini-3.8-flash"]);
    expect(firstOptionLabel(container)).toBe("Connection default (anthropic/claude-sonnet-5)");
    expect(providerCommandIDs(commands)).toEqual(["listAnthropicModels", "listGeminiModels"]);
    expect(container.textContent).not.toContain("OpenAI default model");
    expect(JSON.stringify(commands), "the bundle never names a key field").not.toContain("_api_key");
  });

  it("names the first added provider's default model when the connection has no model", async () => {
    answerHostCommands();
    const container = await renderBundle(hostReady(connection(["openai"], {}), summaryModelTarget));
    await waitForOptions(container, ["", "openai", "openai/gpt-6-sol"]);
    expect(firstOptionLabel(container)).toBe("Connection default (the first added provider's default model)");
  });

  it("lists every provider on a host that reports no auth methods", async () => {
    const commands = answerHostCommands();
    const container = await renderBundle(hostReady({state: "connected", grantedScopes: []}, summaryModelTarget));
    await waitForOptions(container, [
      "", "openai", "anthropic", "gemini", "openai/gpt-6-sol", "anthropic/claude-sonnet-5", "gemini/gemini-3.8-flash",
    ]);
    expect(firstOptionLabel(container)).toBe("Use the connection's default model");
    expect(providerCommandIDs(commands)).toEqual(["listOpenAIModels", "listAnthropicModels", "listGeminiModels"]);
  });
});

describe("the llm Studio bundle on the connection's model field", () => {
  it("offers the added providers' models and saves the default model with use.configuration.save", async () => {
    const commands = answerHostCommands();
    const container = await renderBundle(hostReady(connection(["gemini", "openai"], {}), connectionModelTarget()));
    await waitForOptions(container, ["", "openai", "gemini", "openai/gpt-6-sol", "gemini/gemini-3.8-flash"]);
    expect(firstOptionLabel(container)).toBe("Connector default (the first added provider's default model)");
    expect(container.querySelector<HTMLInputElement>('input[type="radio"][value=""]')?.checked).toBe(true);
    expect(providerCommandIDs(commands)).toEqual(["listOpenAIModels", "listGeminiModels"]);

    await act(async () => { container.querySelector<HTMLInputElement>('input[type="radio"][value="gemini/gemini-3.8-flash"]')?.click(); });
    await act(async () => { saveButton(container).click(); });
    const saves = commands.filter((command) => command.command === "use.configuration.save");
    expect(saves.map((command) => command.input)).toEqual([{value: {model: "gemini/gemini-3.8-flash"}}]);
  });

  it("shows the saved connection model as selected", async () => {
    answerHostCommands();
    const container = await renderBundle(hostReady(connection(["anthropic"], {model: "anthropic/claude-sonnet-5"}),
      connectionModelTarget({model: "anthropic/claude-sonnet-5"})));
    await waitForOptions(container, ["", "anthropic", "anthropic/claude-sonnet-5"]);
    expect(container.querySelector<HTMLInputElement>('input[type="radio"][value="anthropic/claude-sonnet-5"]')?.checked).toBe(true);
  });

  it("keeps the connection setup surface a status card that lists no models", async () => {
    const commands = answerHostCommands();
    const container = await renderBundle(hostReady(connection(["openai"], {}), {kind: "connection"}));
    expect(container.textContent).toContain("Connected.");
    expect(container.querySelector('input[type="radio"]')).toBeNull();
    expect(commands).toEqual([]);
  });
});
