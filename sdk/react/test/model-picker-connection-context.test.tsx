// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT
// @vitest-environment happy-dom

import { act, type ReactElement } from "react";
import { createRoot } from "react-dom/client";
import { afterEach, describe, expect, it, vi } from "vitest";

import {
  ModelPickerStudioApp,
  connectorStudioHostAPIVersion,
  savedModel,
  useConnectorStudioClient,
  type ConnectorConnectionView,
  type ConnectorStudioCommand,
  type ConnectorStudioConfigurationUnitTarget,
  type ConnectorStudioConnection,
  type ConnectorStudioConnectionTarget,
  type ConnectorStudioHostReady,
  type ConnectorStudioTarget,
  type ModelListing,
  type ModelPickerBundleConfig,
} from "../src/index.js";
import { loadClaudeModelListing, loadOpenAIModelListing } from "../src/provider-model-lists.js";

(globalThis as {IS_REACT_ACT_ENVIRONMENT?: boolean}).IS_REACT_ACT_ENVIRONMENT = true;

const operationScope = {kind: "operation", operationId: "generateText", flowType: "Summarize", stepType: "GenerateSummary"} as const;
const triggerScope = {kind: "trigger", triggerName: "messageReceived", bindingName: "summarize", flowType: "Summarize"} as const;

const stepTarget = (
  scope: ConnectorStudioConfigurationUnitTarget["scope"] = operationScope, value: Record<string, unknown> = {},
): ConnectorStudioConfigurationUnitTarget => ({
  kind: "configurationUnit", scope, instanceId: "model", unitId: "modelPicker", label: "Summary model", required: false,
  bindings: [{port: "model", jsonPointer: "/model"}], value,
});

const connectionModelTarget = (value: Record<string, unknown> = {}, unitId = "modelPicker"): ConnectorStudioConnectionTarget => ({
  kind: "connection", unitId, bindings: [{port: "model", jsonPointer: "/model"}], value,
});

// Hosts that predate the connection context send exactly this connection.
const oldHostConnection: ConnectorConnectionView = {state: "connected", grantedScopes: [], detail: "Ready"};

const newHostConnection = (authMethodIds: string[], configuration: Record<string, unknown>): ConnectorConnectionView => ({
  ...oldHostConnection, authMethodIds, configuration,
});

const hostReady = (connection: ConnectorConnectionView, target: ConnectorStudioTarget): ConnectorStudioHostReady => ({
  type: "connector.host.ready", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: "nonce", connectorId: "llm",
  capabilities: ["use.configuration.write", "llm.models-list"], connection, target,
});

const listing: ModelListing = {models: [{id: "openai/gpt-6-sol", label: "GPT-6 Sol"}, {id: "anthropic/claude-sonnet-5"}]};

const providerDefaultDescription = "the provider's default model";

const mountedContainers: {unmount(): void}[] = [];

afterEach(() => {
  for (const container of mountedContainers.splice(0)) container.unmount();
  document.getElementById("dex-connector-studio-theme")?.remove();
  vi.restoreAllMocks();
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

function sendFromHost(message: unknown): void {
  window.dispatchEvent(new MessageEvent("message", {data: message, source: window.parent}));
}

async function renderBundle(config: Partial<ModelPickerBundleConfig>, ready: ConnectorStudioHostReady): Promise<HTMLElement> {
  return renderInDocument(
    <ModelPickerStudioApp connectorId="llm" loadModels={async () => listing} providerName="LLM" {...config}/>,
    () => sendFromHost(ready));
}

/** answerHostCommands records each command the bundle posts and replies later with answer's value, as a host does. */
function answerHostCommands(answer: (command: ConnectorStudioCommand) => Record<string, unknown>): ConnectorStudioCommand[] {
  const commands: ConnectorStudioCommand[] = [];
  vi.spyOn(window.parent, "postMessage").mockImplementation(((message: {type?: unknown}) => {
    if (message.type !== "connector.command") return;
    const command = message as ConnectorStudioCommand;
    commands.push(command);
    queueMicrotask(() => sendFromHost({
      type: "connector.command.result", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: command.sessionNonce,
      connectorId: command.connectorId, requestId: command.requestId, ok: true, value: answer(command),
    }));
  }) as typeof window.postMessage);
  return commands;
}

async function waitForBundle(assertion: () => void): Promise<void> {
  await vi.waitFor(async () => {
    await act(async () => {});
    assertion();
  });
}

function defaultModelOption(container: HTMLElement): {label: string | null | undefined; value: string; isChecked: boolean} {
  const option = container.querySelector(".studio-option");
  const radio = option?.querySelector<HTMLInputElement>('input[type="radio"]');
  if (!option || !radio) throw new Error("the first model option is missing");
  return {label: option.querySelector(".studio-option-label")?.textContent, value: radio.value, isChecked: radio.checked};
}

function modelRadio(container: HTMLElement, model: string): HTMLInputElement {
  const radio = container.querySelector<HTMLInputElement>(`input[type="radio"][value="${model}"]`);
  if (!radio) throw new Error(`model option ${JSON.stringify(model)} is missing`);
  return radio;
}

function saveButton(container: HTMLElement): HTMLButtonElement {
  const button = [...container.querySelectorAll<HTMLButtonElement>("button")].find((candidate) => candidate.textContent === "Save");
  if (!button) throw new Error("Save button is missing");
  return button;
}

function ConnectionProbe(): ReactElement {
  const client = useConnectorStudioClient("llm");
  return <output>{JSON.stringify(client.ready?.connection ?? null)}</output>;
}

describe("useConnectorStudioClient connection context", () => {
  it("reports an old host's omitted auth methods and configuration as empty", async () => {
    const container = await renderInDocument(<ConnectionProbe/>, () => sendFromHost(hostReady(oldHostConnection, {kind: "connection"})));
    expect(JSON.parse(container.textContent ?? "")).toEqual({
      ...oldHostConnection, authMethodIds: [], configuration: {}, isConfigurationReported: false,
    });
  });

  it("passes the host's auth methods and configuration through", async () => {
    for (const [authMethodIds, configuration] of [[["anthropic", "gemini"], {model: "anthropic/claude-sonnet-5"}], [[], {}]] as const) {
      const container = await renderInDocument(<ConnectionProbe/>,
        () => sendFromHost(hostReady(newHostConnection([...authMethodIds], {...configuration}), {kind: "connection"})));
      expect(JSON.parse(container.textContent ?? "")).toEqual({
        ...oldHostConnection, authMethodIds, configuration, isConfigurationReported: true,
      });
    }
  });
});

describe("ModelPickerStudioApp model loaders", () => {
  it("receive the ready message's connection, with no auth methods from an old host", async () => {
    for (const [connection, expectedAuthMethodIds] of [
      [newHostConnection(["anthropic"], {}), ["anthropic"]],
      [oldHostConnection, []],
    ] as const) {
      const connections: ConnectorStudioConnection[] = [];
      await renderBundle({loadModels: async (_client, received) => { connections.push(received); return listing; }}, hostReady(connection, stepTarget()));
      expect(connections.map((received) => received.authMethodIds)).toEqual([expectedAuthMethodIds]);
    }
  });

  it("run again when the connection's auth methods change, and not for a repeated ready message", async () => {
    const authMethodIdLists: string[][] = [];
    const loadModels = async (_client: unknown, connection: ConnectorStudioConnection) => {
      authMethodIdLists.push(connection.authMethodIds);
      return listing;
    };
    await renderBundle({loadModels}, hostReady(newHostConnection(["anthropic"], {}), stepTarget()));
    await act(async () => { sendFromHost(hostReady(newHostConnection(["anthropic"], {}), stepTarget())); });
    await act(async () => { sendFromHost(hostReady(newHostConnection(["anthropic", "gemini"], {}), stepTarget())); });
    expect(authMethodIdLists).toEqual([["anthropic"], ["anthropic", "gemini"]]);
  });
});

describe("ModelPickerStudioApp Step connection default", () => {
  for (const scope of [operationScope, triggerScope]) {
    it(`names the connection's model in the first option of a ${scope.kind} unit and saves it as empty`, async () => {
      const commands = answerHostCommands(() => ({}));
      const container = await renderBundle({defaultModelDescription: providerDefaultDescription},
        hostReady(newHostConnection(["anthropic"], {model: " anthropic/claude-sonnet-5 "}), stepTarget(scope)));
      expect(defaultModelOption(container)).toEqual({label: "Connection default (anthropic/claude-sonnet-5)", value: "", isChecked: true});
      await act(async () => { saveButton(container).click(); });
      expect(commands.map((command) => [command.command, command.input])).toEqual([["use.configuration.save", {value: {model: ""}}]]);
    });

    it(`keeps the generic first option of a ${scope.kind} unit when the host reports no configuration`, async () => {
      const container = await renderBundle({defaultModelDescription: providerDefaultDescription}, hostReady(oldHostConnection, stepTarget(scope)));
      expect(defaultModelOption(container)).toEqual({label: "Use the connection's default model", value: "", isChecked: true});
    });
  }

  it("names the connector default when the connection has no model", async () => {
    const container = await renderBundle({defaultModelDescription: providerDefaultDescription},
      hostReady(newHostConnection(["anthropic"], {model: ""}), stepTarget(operationScope, {model: "openai/gpt-6-sol"})));
    expect(defaultModelOption(container)).toEqual({label: `Connection default (${providerDefaultDescription})`, value: "", isChecked: false});
    expect(modelRadio(container, "openai/gpt-6-sol").checked).toBe(true);
  });

  it("keeps the generic first option when neither a connection model nor a description names the default", async () => {
    const container = await renderBundle({}, hostReady(newHostConnection(["anthropic"], {}), stepTarget()));
    expect(defaultModelOption(container).label).toBe("Use the connection's default model");
  });
});

describe("ModelPickerStudioApp connection target", () => {
  it("renders the setup status card for the connection surface without listing models", async () => {
    let loadCount = 0;
    const container = await renderBundle({loadModels: async () => { loadCount += 1; return listing; }},
      hostReady(newHostConnection(["anthropic"], {model: "anthropic/claude-sonnet-5"}), {kind: "connection"}));
    expect(container.textContent).toContain("Enter the API key in the form above.");
    expect(container.textContent).toContain("Connected. Models are listed live from LLM.");
    expect(container.querySelector('input[type="radio"]')).toBeNull();
    expect(loadCount).toBe(0);
  });

  it("picks the connection's model for the bound model port and saves it with use.configuration.save", async () => {
    const commands = answerHostCommands(() => ({}));
    const connections: ConnectorStudioConnection[] = [];
    const container = await renderBundle(
      {defaultModelDescription: providerDefaultDescription, loadModels: async (_client, connection) => { connections.push(connection); return listing; }},
      hostReady(newHostConnection(["openai", "anthropic"], {model: "anthropic/claude-sonnet-5"}), connectionModelTarget({model: "anthropic/claude-sonnet-5"})));
    expect(connections.map((connection) => connection.authMethodIds)).toEqual([["openai", "anthropic"]]);
    expect(container.querySelector("h2")?.textContent).toBe("Default model");
    expect(defaultModelOption(container)).toEqual({label: `Connector default (${providerDefaultDescription})`, value: "", isChecked: false});
    expect(modelRadio(container, "anthropic/claude-sonnet-5").checked).toBe(true);

    await act(async () => { modelRadio(container, "openai/gpt-6-sol").click(); });
    await act(async () => { saveButton(container).click(); });
    expect(commands.map((command) => [command.command, command.input])).toEqual([["use.configuration.save", {value: {model: "openai/gpt-6-sol"}}]]);
    expect(container.textContent).toContain("Saved openai/gpt-6-sol. Restart the application to use it.");

    await act(async () => { modelRadio(container, "").click(); });
    await act(async () => { saveButton(container).click(); });
    expect(commands.at(-1)?.input).toEqual({value: {model: ""}});
    expect(container.textContent).toContain("Saved the connector default.");
  });

  it("labels the empty connection model generically without a description", async () => {
    const container = await renderBundle({}, hostReady(newHostConnection(["anthropic"], {}), connectionModelTarget()));
    expect(defaultModelOption(container)).toEqual({label: "Use the connector's default model", value: "", isChecked: true});
    expect(container.querySelector(".studio-actions .studio-muted")?.textContent).toBe("Uses the connector's default model");
  });

  it("rejects a connection unit other than the model picker", async () => {
    const container = await renderBundle({}, hostReady(newHostConnection(["anthropic"], {}), connectionModelTarget({}, "providerPicker")));
    expect(container.textContent).toBe("Unsupported LLM configuration unit: providerPicker");
  });

  it("reads the saved model by port or at the binding's JSON Pointer", () => {
    const boundTo = (jsonPointer: string, value: Record<string, unknown>): ConnectorStudioConnectionTarget => ({
      kind: "connection", unitId: "modelPicker", bindings: [{port: "model", jsonPointer}], value,
    });
    expect(savedModel(boundTo("/model", {model: " anthropic/claude-sonnet-5 "}))).toBe("anthropic/claude-sonnet-5");
    expect(savedModel(boundTo("/defaultModel", {defaultModel: "gemini"}))).toBe("gemini");
    expect(savedModel(boundTo("/models~1default", {"models/default": "openai"}))).toBe("openai");
    expect(savedModel(boundTo("/model", {}))).toBe("");
    expect(savedModel({kind: "connection"})).toBe("");
  });
});

// This bundle configuration is the "Connection context" example in README.md, verbatim.
const readmeBundleConfig: ModelPickerBundleConfig = {
  connectorId: "llm", providerName: "LLM", iconUrl: "./icon.svg",
  defaultModelDescription: "the provider's default model",
  loadModels: async (client, connection) => {
    switch (connection.configuration.provider) {
      case "openai":
        return loadOpenAIModelListing(client, {capability: "llm.models-list", commandId: "listOpenAIModels"});
      case "anthropic":
        return loadClaudeModelListing(client, {capability: "llm.models-list", commandId: "listAnthropicModels"});
      default: {
        const message = connection.isConfigurationReported
          ? "Save the connection's provider to list its models, or enter a model ID."
          : "This Dex Web release does not report the connection's provider. Enter a model ID.";
        return {models: [], notices: [{tone: "attention", message}]};
      }
    }
  },
};

describe("README connection context example", () => {
  const providerLists: Record<string, Record<string, unknown>> = {
    listOpenAIModels: {data: [{id: "gpt-6-sol", created: 1}]},
    listAnthropicModels: {data: [{id: "claude-sonnet-5", display_name: "Claude Sonnet 5"}], has_more: false},
  };
  const testCases: {
    name: string;
    connection: ConnectorConnectionView;
    commandIds: string[];
    modelIds: string[];
    notice?: string;
    defaultLabel: string;
  }[] = [
    {
      name: "lists only Claude's models for an anthropic connection",
      connection: newHostConnection([], {provider: "anthropic", model: "claude-sonnet-5"}),
      commandIds: ["listAnthropicModels"], modelIds: ["claude-sonnet-5"],
      defaultLabel: "Connection default (claude-sonnet-5)",
    },
    {
      name: "lists only OpenAI's models for an openai connection",
      connection: newHostConnection([], {provider: "openai"}),
      commandIds: ["listOpenAIModels"], modelIds: ["gpt-6-sol"],
      defaultLabel: "Connection default (the provider's default model)",
    },
    {
      name: "runs no command before the provider is saved",
      connection: newHostConnection([], {}),
      commandIds: [], modelIds: [], notice: "Save the connection's provider to list its models, or enter a model ID.",
      defaultLabel: "Connection default (the provider's default model)",
    },
    {
      name: "runs no command on a host that reports no configuration",
      connection: oldHostConnection,
      commandIds: [], modelIds: [], notice: "This Dex Web release does not report the connection's provider. Enter a model ID.",
      defaultLabel: "Use the connection's default model",
    },
  ];

  for (const testCase of testCases) {
    it(testCase.name, async () => {
      const commands = answerHostCommands((command) => providerLists[String(command.input?.commandId)] ?? {});
      const container = await renderInDocument(<ModelPickerStudioApp {...readmeBundleConfig}/>,
        () => sendFromHost(hostReady(testCase.connection, stepTarget())));
      await waitForBundle(() => expect(container.textContent).toContain("Search models"));
      expect([...container.querySelectorAll<HTMLInputElement>('input[type="radio"]')].map((radio) => radio.value)).toEqual(["", ...testCase.modelIds]);
      expect(commands.map((command) => command.input?.commandId)).toEqual(testCase.commandIds);
      if (testCase.notice !== undefined) expect(container.textContent).toContain(testCase.notice);
      expect(defaultModelOption(container).label).toBe(testCase.defaultLabel);
    });
  }
});
