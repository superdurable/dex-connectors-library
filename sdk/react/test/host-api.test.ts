// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";

import { connectorStudioHostAPIVersion, isConnectorStudioMessage } from "../src/index.js";

describe("Connector Studio Host API", () => {
  it("accepts a nonce-bound versioned message", () => {
    expect(isConnectorStudioMessage({
      type: "connector.command",
      protocolVersion: connectorStudioHostAPIVersion,
      sessionNonce: "nonce",
      connectorId: "google-sheets",
      requestId: "request",
      command: "oauth.connect",
    })).toBe(true);
  });

  it("accepts resource and use-configuration commands", () => {
    const common = {protocolVersion: connectorStudioHostAPIVersion, sessionNonce: "nonce", connectorId: "slack", requestId: "request", type: "connector.command"};
    expect(isConnectorStudioMessage({...common, command: "provider.command.execute", input: {commandId: "listChannels", parameters: {cursor: "next"}}})).toBe(true);
    expect(isConnectorStudioMessage({...common, command: "slack.channels.list"})).toBe(false);
    expect(isConnectorStudioMessage({...common, command: "use.configuration.save", input: {value: {channelId: "C123"}}})).toBe(true);
  });

  it("rejects messages without the protocol identity", () => {
    expect(isConnectorStudioMessage({ type: "connector.command", connectorId: "gmail" })).toBe(false);
  });

  it("rejects unknown message types and malformed ready messages", () => {
    const ready = {
      type: "connector.host.ready",
      protocolVersion: connectorStudioHostAPIVersion,
      sessionNonce: "nonce",
      connectorId: "fixture",
      capabilities: ["configuration.write"],
      connection: { state: "connected", grantedScopes: [] },
      target: {kind: "connection"},
    };
    expect(isConnectorStudioMessage(ready)).toBe(true);
    expect(isConnectorStudioMessage({ ...ready, type: "connector.host.evil" })).toBe(false);
    expect(isConnectorStudioMessage({ ...ready, capabilities: ["safe", 7] })).toBe(false);
  });

  it("accepts an isolated operation unit target", () => {
    expect(isConnectorStudioMessage({
      type: "connector.host.ready",
      protocolVersion: connectorStudioHostAPIVersion,
      sessionNonce: "nonce",
      connectorId: "slack",
      capabilities: ["use.configuration.write", "slack.channels-list"],
      connection: {state: "connected", grantedScopes: []},
      target: {
        kind: "configurationUnit",
        scope: {kind: "operation", operationId: "postChannelMessage", flowType: "ApprovalFlow", stepType: "RequestApproval"},
        instanceId: "approvalChannel",
        unitId: "channelPicker",
        label: "Approval channel",
        required: true,
        bindings: [{port: "channelId", jsonPointer: "/channelId"}],
        value: {channelId: "C123"},
      },
    })).toBe(true);
  });

  it("accepts the connection context and rejects a malformed one", () => {
    const ready = {
      type: "connector.host.ready",
      protocolVersion: connectorStudioHostAPIVersion,
      sessionNonce: "nonce",
      connectorId: "llm",
      capabilities: [],
      target: {kind: "connection"},
    };
    const connection = {state: "connected", grantedScopes: []};
    expect(isConnectorStudioMessage({...ready, connection})).toBe(true);
    expect(isConnectorStudioMessage({...ready, connection: {...connection, authMethodIds: [], configuration: {}}})).toBe(true);
    expect(isConnectorStudioMessage({
      ...ready, connection: {...connection, authMethodIds: ["anthropic", "gemini"], configuration: {model: "anthropic/claude-sonnet-5"}},
    })).toBe(true);
    for (const authMethodIds of ["anthropic", [""], ["anthropic", 7], null]) {
      expect(isConnectorStudioMessage({...ready, connection: {...connection, authMethodIds}}), JSON.stringify(authMethodIds)).toBe(false);
    }
    for (const configuration of [["model"], "model", null]) {
      expect(isConnectorStudioMessage({...ready, connection: {...connection, configuration}}), JSON.stringify(configuration)).toBe(false);
    }
    expect(isConnectorStudioMessage({...ready, connection: {...connection, storedCredentialFields: ["api_key"]}})).toBe(true);
    for (const storedCredentialFields of ["api_key", [""], [7], null]) {
      expect(isConnectorStudioMessage({...ready, connection: {...connection, storedCredentialFields}}), JSON.stringify(storedCredentialFields)).toBe(false);
    }
  });

  it("accepts the connection.save command", () => {
    expect(isConnectorStudioMessage({
      type: "connector.command", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: "nonce", connectorId: "llm",
      requestId: "request", command: "connection.save",
      input: {configuration: {provider: "anthropic"}, credentials: {api_key: "typed"}, keepCredentialFields: []},
    })).toBe(true);
  });

  it("accepts a connection target for one configuration field's unit, whole or not at all", () => {
    const ready = (target: Record<string, unknown>) => ({
      type: "connector.host.ready",
      protocolVersion: connectorStudioHostAPIVersion,
      sessionNonce: "nonce",
      connectorId: "llm",
      capabilities: ["use.configuration.write"],
      connection: {state: "connected", grantedScopes: [], authMethodIds: ["anthropic"], configuration: {}},
      target,
    });
    const fieldTarget = {kind: "connection", unitId: "modelPicker", bindings: [{port: "model", jsonPointer: "/model"}], value: {model: "anthropic"}};
    expect(isConnectorStudioMessage(ready(fieldTarget))).toBe(true);
    expect(isConnectorStudioMessage(ready({...fieldTarget, value: {}}))).toBe(true);
    expect(isConnectorStudioMessage(ready({kind: "connection"}))).toBe(true);
    expect(isConnectorStudioMessage(ready({...fieldTarget, unitId: ""}))).toBe(false);
    expect(isConnectorStudioMessage(ready({...fieldTarget, unitId: undefined}))).toBe(false);
    expect(isConnectorStudioMessage(ready({...fieldTarget, bindings: undefined}))).toBe(false);
    expect(isConnectorStudioMessage(ready({...fieldTarget, bindings: [{port: "model"}]}))).toBe(false);
    expect(isConnectorStudioMessage(ready({...fieldTarget, value: undefined}))).toBe(false);
    expect(isConnectorStudioMessage(ready({...fieldTarget, value: ["anthropic"]}))).toBe(false);
  });

  it("rejects unknown commands and malformed results", () => {
    const common = { protocolVersion: connectorStudioHostAPIVersion, sessionNonce: "nonce", connectorId: "fixture", requestId: "request" };
    expect(isConnectorStudioMessage({ ...common, type: "connector.command", command: "credential.read" })).toBe(false);
    expect(isConnectorStudioMessage({ ...common, type: "connector.command.result", ok: "yes" })).toBe(false);
  });

  it("accepts bounded frame resize messages", () => {
    const resize = {
      type: "connector.frame.resize",
      protocolVersion: connectorStudioHostAPIVersion,
      sessionNonce: "nonce",
      connectorId: "slack",
      height: 384,
    };
    expect(isConnectorStudioMessage(resize)).toBe(true);
    expect(isConnectorStudioMessage({...resize, height: 0})).toBe(false);
    expect(isConnectorStudioMessage({...resize, height: 4097})).toBe(false);
    expect(isConnectorStudioMessage({...resize, height: 384.5})).toBe(false);
  });
});
