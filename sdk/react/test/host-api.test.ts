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

  it("accepts Slack resource and trigger configuration commands", () => {
    const common = {protocolVersion: connectorStudioHostAPIVersion, sessionNonce: "nonce", connectorId: "slack", requestId: "request", type: "connector.command"};
    expect(isConnectorStudioMessage({...common, command: "slack.channels.list"})).toBe(true);
    expect(isConnectorStudioMessage({...common, command: "slack.users.list"})).toBe(true);
    expect(isConnectorStudioMessage({...common, command: "trigger.configuration.save", input: {bindingName: "approval-start"}})).toBe(true);
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
      configuration: {},
      triggerBindings: {channelThreadCreated: {"approval-start": {channelId: "C123"}}},
    };
    expect(isConnectorStudioMessage(ready)).toBe(true);
    expect(isConnectorStudioMessage({ ...ready, type: "connector.host.evil" })).toBe(false);
    expect(isConnectorStudioMessage({ ...ready, capabilities: ["safe", 7] })).toBe(false);
  });

  it("rejects unknown commands and malformed results", () => {
    const common = { protocolVersion: connectorStudioHostAPIVersion, sessionNonce: "nonce", connectorId: "fixture", requestId: "request" };
    expect(isConnectorStudioMessage({ ...common, type: "connector.command", command: "credential.read" })).toBe(false);
    expect(isConnectorStudioMessage({ ...common, type: "connector.command.result", ok: "yes" })).toBe(false);
  });
});
