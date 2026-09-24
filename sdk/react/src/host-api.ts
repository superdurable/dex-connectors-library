export const connectorStudioHostAPIVersion = "0.1.0" as const;

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

export interface ConnectorStudioHostReady {
  type: "connector.host.ready";
  protocolVersion: typeof connectorStudioHostAPIVersion;
  sessionNonce: string;
  connectorId: string;
  capabilities: string[];
  connection: ConnectorConnectionView;
  configuration: Record<string, unknown>;
}

export type ConnectorStudioCommandType =
  | "oauth.connect"
  | "oauth.reconnect"
  | "oauth.revoke"
  | "google.picker.open-spreadsheet"
  | "google.sheets.list-tabs"
  | "configuration.save";

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

export type ConnectorStudioMessage =
  | ConnectorStudioHostReady
  | ConnectorStudioCommand
  | ConnectorStudioCommandResult;

const commandTypes = new Set<ConnectorStudioCommandType>([
  "oauth.connect",
  "oauth.reconnect",
  "oauth.revoke",
  "google.picker.open-spreadsheet",
  "google.sheets.list-tabs",
  "configuration.save",
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
      && isRecord(message.configuration);
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
  return false;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isCommandError(value: unknown): boolean {
  return isRecord(value) && typeof value.code === "string" && typeof value.message === "string";
}
