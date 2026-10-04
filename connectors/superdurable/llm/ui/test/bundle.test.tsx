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

const summaryModelTarget = (value: Record<string, unknown> = {}): ConnectorStudioTarget => ({
  kind: "configurationUnit",
  scope: {kind: "operation", operationId: "generateText", flowType: "LLMSummarizeText", stepType: "SummarizeText"},
  instanceId: "summaryModel", unitId: "modelPicker", label: "Summary model", required: false,
  bindings: [{port: "model", jsonPointer: "/model"}], value,
});

// The host renders the modelPicker unit for the connection's model field, as the manifest's studioUnit declares.
const connectionModelTarget = (value: Record<string, unknown> = {}): ConnectorStudioTarget => ({
  kind: "connection", unitId: "modelPicker", bindings: [{port: "model", jsonPointer: "/model"}], value,
});

const connection = (configuration: Record<string, unknown>): ConnectorConnectionView => ({
  state: "connected", grantedScopes: [], authMethodIds: [], configuration,
});

const hostReady = (connectionView: ConnectorConnectionView, target: ConnectorStudioTarget): ConnectorStudioHostReady => ({
  type: "connector.host.ready", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: "llm-bundle-nonce", connectorId: "llm",
  capabilities: ["use.configuration.write", "llm.models-list"], connection: connectionView, target,
});

const providerLists: Record<string, Record<string, unknown>> = {
  listAnthropicModels: {data: [{id: "claude-sonnet-5", display_name: "Claude Sonnet 5"}, {id: "claude-opus-5-5"}], has_more: false},
  listDeepSeekModels: {data: [{id: "deepseek-flash", name: "DeepSeek-V4.1-Flash"}, {id: "deepseek-v4-pro"}]},
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

async function typeModel(container: HTMLElement, model: string): Promise<void> {
  const input = container.querySelector<HTMLInputElement>('input[type="text"]');
  if (!input) throw new Error("model ID entry is missing");
  await act(async () => {
    const setValue = Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set;
    setValue?.call(input, model);
    input.dispatchEvent(new Event("input", {bubbles: true}));
  });
}

function providerCommandIDs(commands: ConnectorStudioCommand[]): string[] {
  return commands.filter((command) => command.command === "provider.command.execute").map((command) => String(command.input?.commandId));
}

describe("the llm Studio bundle on a Step", () => {
  it("lists the connection provider's models and names the connection's model in the first option", async () => {
    const commands = answerHostCommands();
    const container = await renderBundle(hostReady(connection({provider: "anthropic", model: "claude-opus-5-5"}), summaryModelTarget()));
    await waitForOptions(container, ["", "claude-sonnet-5", "claude-opus-5-5"]);
    expect(firstOptionLabel(container)).toBe("Connection default (claude-opus-5-5)");
    expect(providerCommandIDs(commands)).toEqual(["listAnthropicModels"]);
    expect(JSON.stringify(commands), "the bundle never names the key field").not.toContain("api_key");
  });

  it("names the provider's default model when the connection has no model, and saves a bare model ID", async () => {
    const commands = answerHostCommands();
    const container = await renderBundle(hostReady(connection({provider: "deepseek"}), summaryModelTarget()));
    await waitForOptions(container, ["", "deepseek-flash", "deepseek-v4-pro"]);
    expect(firstOptionLabel(container)).toBe("Connection default (the provider's default model)");
    expect(providerCommandIDs(commands)).toEqual(["listDeepSeekModels"]);

    await act(async () => { container.querySelector<HTMLInputElement>('input[type="radio"][value="deepseek-v4-pro"]')?.click(); });
    await act(async () => { saveButton(container).click(); });
    const saves = commands.filter((command) => command.command === "use.configuration.save");
    expect(saves.map((command) => command.input)).toEqual([{value: {model: "deepseek-v4-pro"}}]);
  });

  it("offers model ID entry when the provider's list fails, and blocks an ID with spaces", async () => {
    const commands = answerHostCommands();
    const container = await renderBundle(hostReady(connection({provider: "kimi", region: "china"}), summaryModelTarget()));
    await vi.waitFor(async () => {
      await act(async () => {});
      expect(container.textContent).toContain("The Kimi model list failed");
    });
    expect(providerCommandIDs(commands)).toEqual(["listKimiModelsChina"]);
    await typeModel(container, "kimi k3");
    expect(saveButton(container).disabled).toBe(true);
    await typeModel(container, " kimi-k3 ");
    expect(saveButton(container).disabled).toBe(false);
    await act(async () => { saveButton(container).click(); });
    const saves = commands.filter((command) => command.command === "use.configuration.save");
    expect(saves.map((command) => command.input)).toEqual([{value: {model: "kimi-k3"}}]);
  });

  it("runs no provider command before the connection saves a provider", async () => {
    const commands = answerHostCommands();
    const container = await renderBundle(hostReady(connection({}), summaryModelTarget({model: "gpt-6-luna"})));
    await waitForOptions(container, [""]);
    expect(container.textContent).toContain("Save the connection with its provider and api_key first");
    expect(providerCommandIDs(commands)).toEqual([]);
    expect(container.querySelector<HTMLInputElement>('input[type="text"]')?.value, "a saved pick stays editable").toBe("gpt-6-luna");
  });
});

describe("the llm Studio bundle on the connection's model field", () => {
  it("offers the provider's models and saves the default model with use.configuration.save", async () => {
    const commands = answerHostCommands();
    const container = await renderBundle(hostReady(connection({provider: "anthropic"}), connectionModelTarget()));
    await waitForOptions(container, ["", "claude-sonnet-5", "claude-opus-5-5"]);
    expect(firstOptionLabel(container)).toBe("Connector default (the provider's default model)");
    expect(container.querySelector<HTMLInputElement>('input[type="radio"][value=""]')?.checked).toBe(true);

    await act(async () => { container.querySelector<HTMLInputElement>('input[type="radio"][value="claude-opus-5-5"]')?.click(); });
    await act(async () => { saveButton(container).click(); });
    const saves = commands.filter((command) => command.command === "use.configuration.save");
    expect(saves.map((command) => command.input)).toEqual([{value: {model: "claude-opus-5-5"}}]);
  });

  it("shows the saved connection model as selected", async () => {
    answerHostCommands();
    const container = await renderBundle(hostReady(connection({provider: "anthropic", model: "claude-sonnet-5"}),
      connectionModelTarget({model: "claude-sonnet-5"})));
    await waitForOptions(container, ["", "claude-sonnet-5", "claude-opus-5-5"]);
    expect(container.querySelector<HTMLInputElement>('input[type="radio"][value="claude-sonnet-5"]')?.checked).toBe(true);
  });

  it("keeps the connection setup surface a status card that lists no models", async () => {
    const commands = answerHostCommands();
    const container = await renderBundle(hostReady(connection({provider: "openai"}), {kind: "connection"}));
    expect(container.textContent).toContain("Connected.");
    expect(container.querySelector('input[type="radio"]')).toBeNull();
    expect(commands).toEqual([]);
  });
});
