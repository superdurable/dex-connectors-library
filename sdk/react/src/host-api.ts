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
}

export interface ConnectorStudioConnectionTarget {
  kind: "connection";
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
}

export type ConnectorStudioCommandType =
  | "oauth.connect"
  | "oauth.reconnect"
  | "oauth.revoke"
  | "google.picker.open-spreadsheet"
  | "google.sheets.list-tabs"
  | "slack.channels.list"
  | "slack.users.list"
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
  "google.picker.open-spreadsheet",
  "google.sheets.list-tabs",
  "slack.channels.list",
  "slack.users.list",
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
      && isRecord(message.connection)
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

function isConnectorStudioTarget(value: unknown): value is ConnectorStudioTarget {
  if (!isRecord(value) || typeof value.kind !== "string") return false;
  if (value.kind === "connection") return true;
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
    && Array.isArray(value.bindings)
    && value.bindings.every((binding) => isRecord(binding) && nonEmptyString(binding.port) && nonEmptyString(binding.jsonPointer))
    && isRecord(value.value);
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
