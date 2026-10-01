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

export interface ConfluenceSetupViewProps { connection: ConnectorConnectionView; onConnect(): void; onReconnect(): void; }

const reconnectReasons: Partial<Record<ConnectorConnectionState, string>> = {
  expired: "Connection expired",
  revoked: "Connection revoked",
  insufficient_scope: "Additional permission required",
};

export function ConfluenceSetupView({connection, onConnect, onReconnect}: ConfluenceSetupViewProps) {
  const reconnectReason = reconnectReasons[connection.state];
  return <StudioSurface label="Confluence connection">
    <StudioHeader description="Authorize one Atlassian account and pick its Confluence site on the consent screen. Each Flow Step picks its own space." iconUrl="./icon.svg" title="Confluence"/>
    {connection.state === "connected" && <StudioNotice tone="success">
      Connected{connection.accountEmail ? <> as <strong>{connection.accountEmail}</strong></> : ""}. Choose the Confluence site in the connection form.
    </StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onConnect} variant="primary">Connect Confluence</StudioButton></div>
    </>}
    {reconnectReason && <>
      <StudioNotice tone="attention">{reconnectReason}. Authorize Confluence again, pick the same site, and accept every requested scope.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onReconnect} variant="primary">Reconnect Confluence</StudioButton></div>
    </>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The Confluence connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
