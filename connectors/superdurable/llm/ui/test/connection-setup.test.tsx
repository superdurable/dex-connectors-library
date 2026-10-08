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
} from "@superdurable/dex-connectors-react";

import { llmModelPickerBundleConfig } from "../src/bundle-config.js";

(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true;

const setupCapabilities = ["use.configuration.write", "llm.models-list", "connection.write"];

const connection = (configuration: Record<string, unknown>, storedCredentialFields: string[] = []): ConnectorConnectionView => ({
  state: storedCredentialFields.length > 0 ? "connected" : "not_configured", grantedScopes: [], authMethodIds: [],
  configuration, storedCredentialFields,
});

const setupReady = (connectionView: ConnectorConnectionView, capabilities = setupCapabilities): ConnectorStudioHostReady => ({
  type: "connector.host.ready", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: "llm-setup-nonce", connectorId: "llm",
  capabilities, connection: connectionView, target: {kind: "connection"},
});

const providerLists: Record<string, Record<string, unknown>> = {
  listAnthropicModels: {data: [{id: "claude-sonnet-5", display_name: "Claude Sonnet 5"}, {id: "claude-opus-5-5"}], has_more: false},
  listDeepSeekModels: {data: [{id: "deepseek-flash"}, {id: "deepseek-v4-pro"}]},
};

const unmountCallbacks: Array<() => void> = [];

afterEach(() => {
  for (const unmount of unmountCallbacks.splice(0)) unmount();
  document.getElementById("dex-connector-studio-theme")?.remove();
  vi.useRealTimers();
  vi.restoreAllMocks();
});

function sendFromHost(message: unknown): void {
  window.dispatchEvent(new MessageEvent("message", {data: message, source: window.parent}));
}

/** answerHostCommands answers provider lists by command ID, fails unknown lists, and accepts saves. */
function answerHostCommands(): ConnectorStudioCommand[] {
  const commands: ConnectorStudioCommand[] = [];
  vi.spyOn(window.parent, "postMessage").mockImplementation(((message: {type?: unknown}) => {
    if (message.type !== "connector.command") return;
    const command = message as ConnectorStudioCommand;
    commands.push(command);
    const value = command.command === "provider.command.execute" ? providerLists[String(command.input?.commandId ?? "")] : {};
    queueMicrotask(() => sendFromHost({
      type: "connector.command.result", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: command.sessionNonce,
      connectorId: command.connectorId, requestId: command.requestId,
      ...(value ? {ok: true, value} : {ok: false, error: {code: "COMMAND_FAILED", message: "Connector provider command failed"}}),
    }));
  }) as typeof window.postMessage);
  return commands;
}

async function renderSetup(ready: ConnectorStudioHostReady): Promise<HTMLElement> {
  const container = document.createElement("div");
  document.body.append(container);
  const root = createRoot(container);
  unmountCallbacks.push(() => { act(() => root.unmount()); container.remove(); });
  await act(async () => { root.render(<ModelPickerStudioApp {...llmModelPickerBundleConfig}/>); });
  await act(async () => { sendFromHost(ready); });
  return container;
}

function fieldControl<T extends HTMLElement>(container: HTMLElement, label: string, selector: string): T {
  const field = Array.from(container.querySelectorAll(".studio-field")).find((candidate) => candidate.querySelector("label > span")?.textContent === label);
  const control = field?.querySelector<T>(selector);
  if (!control) throw new Error(`field ${label} is missing`);
  return control;
}

function hasField(container: HTMLElement, label: string): boolean {
  return Array.from(container.querySelectorAll(".studio-field label > span")).some((span) => span.textContent === label);
}

async function choose(control: HTMLSelectElement, value: string): Promise<void> {
  await act(async () => {
    control.value = value;
    control.dispatchEvent(new Event("change", {bubbles: true}));
  });
}

async function type(control: HTMLInputElement, text: string): Promise<void> {
  await act(async () => {
    Object.getOwnPropertyDescriptor(HTMLInputElement.prototype, "value")?.set?.call(control, text);
    control.dispatchEvent(new Event("input", {bubbles: true}));
  });
}

/** settle lets the typed-key delay pass and every command result arrive. */
async function settle(): Promise<void> {
  await act(async () => { await vi.advanceTimersByTimeAsync(800); });
  await act(async () => {});
}

function saveButton(container: HTMLElement): HTMLButtonElement {
  const button = Array.from(container.querySelectorAll<HTMLButtonElement>("button")).find((candidate) => candidate.textContent === "Save");
  if (!button) throw new Error("Save button is missing");
  return button;
}

function modelOptionValues(container: HTMLElement): string[] {
  return Array.from(fieldControl<HTMLSelectElement>(container, "Model", "select").options).map((option) => option.value);
}

describe("the llm connection setup", () => {
  it("chooses a provider, lists its models with the typed key, and saves everything in one command", async () => {
    vi.useFakeTimers({shouldAdvanceTime: true});
    const commands = answerHostCommands();
    const container = await renderSetup(setupReady(connection({})));
    expect(hasField(container, "Model"), "the model waits for a provider").toBe(false);
    await choose(fieldControl(container, "Provider", "select"), "anthropic");
    expect(saveButton(container).disabled, "a new connection needs a key").toBe(true);
    await type(fieldControl(container, "Claude API key", "input"), "  sk-ant-typed  ");
    await settle();
    expect(modelOptionValues(container)).toEqual(["", "claude-sonnet-5", "claude-opus-5-5", "__other_model__"]);
    await choose(fieldControl(container, "Model", "select"), "claude-opus-5-5");
    await act(async () => { saveButton(container).click(); });
    expect(commands.map((command) => command.command)).toEqual(["provider.command.execute", "connection.save"]);
    expect(commands[0]?.input).toMatchObject({commandId: "listAnthropicModels", credentials: {api_key: "sk-ant-typed"}});
    expect(commands[1]?.input).toEqual({
      configuration: {provider: "anthropic", model: "claude-opus-5-5"}, credentials: {api_key: "sk-ant-typed"}, keepCredentialFields: [],
    });
    await vi.waitFor(async () => {
      await act(async () => {});
      expect(container.textContent).toContain("Saved. Restart the application");
    });
  });

  it("keeps the saved key for the saved provider and lists with it", async () => {
    vi.useFakeTimers({shouldAdvanceTime: true});
    const commands = answerHostCommands();
    const container = await renderSetup(setupReady(connection({provider: "deepseek", model: "deepseek-v4-pro"}, ["api_key"])));
    await settle();
    expect(fieldControl<HTMLSelectElement>(container, "Model", "select").value).toBe("deepseek-v4-pro");
    expect(fieldControl<HTMLInputElement>(container, "DeepSeek API key", "input").placeholder).toContain("leave blank to keep");
    expect(commands[0]?.input).toEqual({commandId: "listDeepSeekModels", parameters: {}});
    await act(async () => { saveButton(container).click(); });
    expect(commands.filter((command) => command.command === "connection.save").map((command) => command.input)).toEqual([{
      configuration: {provider: "deepseek", model: "deepseek-v4-pro"}, credentials: {}, keepCredentialFields: ["api_key"],
    }]);
  });

  it("never lists a new provider with the previous provider's typed key", async () => {
    vi.useFakeTimers({shouldAdvanceTime: true});
    const commands = answerHostCommands();
    const container = await renderSetup(setupReady(connection({provider: "anthropic"}, ["api_key"])));
    await type(fieldControl(container, "Claude API key", "input"), "sk-ant-typed");
    await choose(fieldControl(container, "Provider", "select"), "deepseek");
    await settle();
    expect(fieldControl<HTMLInputElement>(container, "DeepSeek API key", "input").value).toBe("");
    expect(commands.filter((command) => command.input?.commandId === "listDeepSeekModels")).toEqual([]);
    expect(container.textContent).toContain("Enter the API key to list DeepSeek models.");
    expect(saveButton(container).disabled, "the saved key belongs to Claude").toBe(true);
  });

  it("shows the region only for providers with several platforms and the workspace only for Claude", async () => {
    answerHostCommands();
    const container = await renderSetup(setupReady(connection({})));
    await choose(fieldControl(container, "Provider", "select"), "qwen");
    expect(Array.from(fieldControl<HTMLSelectElement>(container, "Region", "select").options).map((option) => option.value))
      .toEqual(["global", "hong-kong", "china"]);
    expect(hasField(container, "Workspace ID (optional)")).toBe(false);
    await choose(fieldControl(container, "Provider", "select"), "anthropic");
    expect(hasField(container, "Region")).toBe(false);
    expect(hasField(container, "Workspace ID (optional)")).toBe(true);
  });

  it("lets a model ID be entered when the list fails, and checks it", async () => {
    vi.useFakeTimers({shouldAdvanceTime: true});
    const commands = answerHostCommands();
    const container = await renderSetup(setupReady(connection({})));
    await choose(fieldControl(container, "Provider", "select"), "mistral");
    await choose(fieldControl(container, "Region", "select"), "eu");
    await type(fieldControl(container, "Mistral API key", "input"), "typed-key");
    await settle();
    expect(container.textContent).toContain("The Mistral model list failed");
    await choose(fieldControl(container, "Model", "select"), "__other_model__");
    await type(fieldControl(container, "Model ID", "input"), "mistral large");
    expect(saveButton(container).disabled).toBe(true);
    await type(fieldControl(container, "Model ID", "input"), "mistral-medium-2604");
    await act(async () => { saveButton(container).click(); });
    expect(commands.filter((command) => command.command === "connection.save").map((command) => command.input)).toEqual([{
      configuration: {provider: "mistral", region: "eu", model: "mistral-medium-2604"}, credentials: {api_key: "typed-key"}, keepCredentialFields: [],
    }]);
  });

  it("stays a status card on a host that does not grant connection.write", async () => {
    const commands = answerHostCommands();
    const container = await renderSetup(setupReady(connection({provider: "openai"}, ["api_key"]), ["use.configuration.write", "llm.models-list"]));
    expect(container.textContent).toContain("Enter the API key in the form above.");
    expect(container.querySelector("select")).toBeNull();
    expect(commands).toEqual([]);
  });
});
