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
