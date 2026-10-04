// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export const connectorStudioHostAPIVersion = "0.2.0" as const;

export type ConnectorConnectionState =
  | "not_configured"
  | "authorization_pending"
  | "connected"
  | "expired"
  | "revoked"
  | "insufficient_scope"
  | "broker_unavailable"
  | "error";

export interface ConnectorConnectionView {
  connectionId?: string;
  state: ConnectorConnectionState;
  accountEmail?: string;
  grantedScopes: string[];
  detail?: string;
  /**
   * authMethodIds holds the manifest auth method ID the connection selected,
   * or is empty before one is chosen. Hosts that predate the field omit it;
   * ConnectorStudioClient.ready reports an empty list for them.
   */
  authMethodIds?: string[];
  /**
   * configuration is the connection's stored non-secret configuration, keyed
   * by field name, such as {provider: "anthropic", model: "claude-sonnet-5"}.
   * It never carries credential fields. Hosts that predate the field omit it;
   * ConnectorStudioClient.ready reports an empty object for them.
   */
  configuration?: Record<string, unknown>;
}

/**
 * ConnectorStudioConnectionTarget selects the connection surface. Without
 * unitId it is the connection setup surface. With unitId, the host renders
 * that Studio unit inline in the connection form for one connection
 * configuration field, and bindings and value are present.
 */
export interface ConnectorStudioConnectionTarget {
  kind: "connection";
  /** unitId is the manifest Studio unit that renders the field, such as "modelPicker". */
  unitId?: string;
  /** bindings binds the unit's output port to the field, such as [{port: "model", jsonPointer: "/model"}]. */
  bindings?: {port: string; jsonPointer: string}[];
  /** value holds the field's stored value, such as {model: "anthropic/claude-sonnet-5"}. */
  value?: Record<string, unknown>;
}

export interface ConnectorStudioOperationScope {
  kind: "operation";
  operationId: string;
  flowType: string;
  stepType: string;
}

export interface ConnectorStudioTriggerScope {
  kind: "trigger";
  triggerName: string;
  bindingName: string;
  flowType: string;
}

export interface ConnectorStudioConfigurationUnitTarget {
  kind: "configurationUnit";
  scope: ConnectorStudioOperationScope | ConnectorStudioTriggerScope;
  instanceId: string;
  unitId: string;
  label: string;
  description?: string;
  required: boolean;
  bindings: {port: string; jsonPointer: string}[];
  value: Record<string, unknown>;
}

export type ConnectorStudioTarget = ConnectorStudioConnectionTarget | ConnectorStudioConfigurationUnitTarget;

export interface ConnectorStudioHostReady {
  type: "connector.host.ready";
  protocolVersion: typeof connectorStudioHostAPIVersion;
  sessionNonce: string;
  connectorId: string;
  capabilities: string[];
  connection: ConnectorConnectionView;
  target: ConnectorStudioTarget;
  /** theme is the host's colour theme. Bundles use "light" when it is absent or unknown. */
  theme?: "light" | "dark";
  /**
   * themeTokens are the host's current values for allowlisted --studio-* CSS
   * custom properties, keyed by property name. Bundles apply only names in
   * connectorStudioThemeTokenNames whose values pass
   * isConnectorStudioThemeTokenValue, and keep their embedded defaults for
   * every other token.
   */
  themeTokens?: Record<string, string>;
  /**
   * stylesheet is the host's canonical Studio stylesheet, re-sent with every
   * ready message. It styles the connectorStudioClassNames contract and
   * declares the token defaults for both themes. Bundles install it in place
   * of the connectorStudioStyles they were built with when it passes
   * isConnectorStudioStylesheet, so a Dex Web restyle reaches released bundles.
   * Bundles keep connectorStudioStyles when it is absent or rejected.
   */
  stylesheet?: string;
}

export type ConnectorStudioCommandType =
  | "oauth.connect"
  | "oauth.reconnect"
  | "oauth.revoke"
  | "provider.command.execute"
  | "use.configuration.save";

export interface ConnectorStudioCommand {
  type: "connector.command";
  protocolVersion: typeof connectorStudioHostAPIVersion;
  sessionNonce: string;
  connectorId: string;
  requestId: string;
  command: ConnectorStudioCommandType;
  input?: Record<string, unknown>;
}

export interface ConnectorStudioCommandResult {
  type: "connector.command.result";
  protocolVersion: typeof connectorStudioHostAPIVersion;
  sessionNonce: string;
  connectorId: string;
  requestId: string;
  ok: boolean;
  value?: Record<string, unknown>;
  error?: { code: string; message: string };
}

export interface ConnectorStudioFrameResize {
  type: "connector.frame.resize";
  protocolVersion: typeof connectorStudioHostAPIVersion;
  sessionNonce: string;
  connectorId: string;
  height: number;
}

export type ConnectorStudioMessage =
  | ConnectorStudioHostReady
  | ConnectorStudioCommand
  | ConnectorStudioCommandResult
  | ConnectorStudioFrameResize;

const commandTypes = new Set<ConnectorStudioCommandType>([
  "oauth.connect",
  "oauth.reconnect",
  "oauth.revoke",
  "provider.command.execute",
  "use.configuration.save",
]);

export function isConnectorStudioMessage(value: unknown): value is ConnectorStudioMessage {
  if (typeof value !== "object" || value === null) return false;
  const message = value as Record<string, unknown>;
  const common = message.protocolVersion === connectorStudioHostAPIVersion
    && typeof message.type === "string"
    && typeof message.sessionNonce === "string"
    && message.sessionNonce.length > 0
    && typeof message.connectorId === "string"
    && message.connectorId.length > 0;
  if (!common) return false;
  if (message.type === "connector.host.ready") {
    return Array.isArray(message.capabilities)
      && message.capabilities.every((capability) => typeof capability === "string")
      && isConnectorConnectionView(message.connection)
      && isConnectorStudioTarget(message.target);
  }
  if (message.type === "connector.command") {
    return typeof message.requestId === "string"
      && message.requestId.length > 0
      && typeof message.command === "string"
      && commandTypes.has(message.command as ConnectorStudioCommandType)
      && (message.input === undefined || isRecord(message.input));
  }
  if (message.type === "connector.command.result") {
    return typeof message.requestId === "string"
      && message.requestId.length > 0
      && typeof message.ok === "boolean"
      && (message.value === undefined || isRecord(message.value))
      && (message.error === undefined || isCommandError(message.error));
  }
  if (message.type === "connector.frame.resize") {
    return typeof message.height === "number"
      && Number.isInteger(message.height)
      && message.height >= 80
      && message.height <= 4096;
  }
  return false;
}

function isConnectorConnectionView(value: unknown): value is ConnectorConnectionView {
  return isRecord(value)
    && (value.authMethodIds === undefined || (Array.isArray(value.authMethodIds) && value.authMethodIds.every(nonEmptyString)))
    && (value.configuration === undefined || isRecord(value.configuration));
}

function isConnectorStudioTarget(value: unknown): value is ConnectorStudioTarget {
  if (!isRecord(value) || typeof value.kind !== "string") return false;
  if (value.kind === "connection") {
    if (value.unitId === undefined && value.bindings === undefined && value.value === undefined) return true;
    return nonEmptyString(value.unitId) && isPortBindingList(value.bindings) && isRecord(value.value);
  }
  if (value.kind !== "configurationUnit" || !isRecord(value.scope)) return false;
  const scope = value.scope;
  const validScope = scope.kind === "operation"
    ? nonEmptyString(scope.operationId) && nonEmptyString(scope.flowType) && nonEmptyString(scope.stepType)
    : scope.kind === "trigger"
      && nonEmptyString(scope.triggerName) && nonEmptyString(scope.bindingName) && nonEmptyString(scope.flowType);
  return validScope
    && nonEmptyString(value.instanceId)
    && nonEmptyString(value.unitId)
    && nonEmptyString(value.label)
    && typeof value.required === "boolean"
    && isPortBindingList(value.bindings)
    && isRecord(value.value);
}

function isPortBindingList(value: unknown): value is {port: string; jsonPointer: string}[] {
  return Array.isArray(value)
    && value.every((binding) => isRecord(binding) && nonEmptyString(binding.port) && nonEmptyString(binding.jsonPointer));
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function nonEmptyString(value: unknown): value is string {
  return typeof value === "string" && value.length > 0;
}

function isCommandError(value: unknown): boolean {
  return isRecord(value) && typeof value.code === "string" && typeof value.message === "string";
}
