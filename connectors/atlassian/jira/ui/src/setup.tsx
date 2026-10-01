// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StudioButton,
  StudioHeader,
  StudioNotice,
  StudioSurface,
  type ConnectorConnectionState,
  type ConnectorConnectionView,
} from "@superdurable/dex-connectors-react";

export interface JiraSetupViewProps { connection: ConnectorConnectionView; onConnect(): void; onReconnect(): void; }

const reconnectReasons: Partial<Record<ConnectorConnectionState, string>> = {
  expired: "Connection expired",
  revoked: "Connection revoked",
  insufficient_scope: "Additional permission required",
};

export function JiraSetupView({connection, onConnect, onReconnect}: JiraSetupViewProps) {
  const reconnectReason = reconnectReasons[connection.state];
  return <StudioSurface label="Jira connection">
    <StudioHeader description="Authorize one Atlassian account and pick its Jira site on the consent screen. Each Flow Step picks its own project." iconUrl="./icon.svg" title="Jira"/>
    {connection.state === "connected" && <StudioNotice tone="success">
      Connected{connection.accountEmail ? <> as <strong>{connection.accountEmail}</strong></> : ""}. Choose the Jira site in the connection form.
    </StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onConnect} variant="primary">Connect Jira</StudioButton></div>
    </>}
    {reconnectReason && <>
      <StudioNotice tone="attention">{reconnectReason}. Authorize Jira again, pick the same site, and accept every requested scope.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onReconnect} variant="primary">Reconnect Jira</StudioButton></div>
    </>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The Jira connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
