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

export interface ZohoDeskSetupViewProps { connection: ConnectorConnectionView; onConnect(): void; onReconnect(): void; }

const reconnectReasons: Partial<Record<ConnectorConnectionState, string>> = {
  expired: "Connection expired",
  revoked: "Connection revoked",
  insufficient_scope: "Additional permission required",
};

export function ZohoDeskSetupView({connection, onConnect, onReconnect}: ZohoDeskSetupViewProps) {
  const reconnectReason = reconnectReasons[connection.state];
  return <StudioSurface label="Zoho Desk connection">
    <StudioHeader description="Authorize one Zoho Desk agent in the connection's data center, then choose its organization in the connection form." iconUrl="./icon.svg" title="Zoho Desk"/>
    {connection.state === "connected" && <StudioNotice tone="success">
      Connected{connection.accountEmail ? <> as <strong>{connection.accountEmail}</strong></> : ""}. Choose the Zoho Desk organization in the connection form.
    </StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onConnect} variant="primary">Connect Zoho Desk</StudioButton></div>
    </>}
    {reconnectReason && <>
      <StudioNotice tone="attention">{reconnectReason}. Authorize Zoho Desk again, pick the same organization, and accept every requested scope.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onReconnect} variant="primary">Reconnect Zoho Desk</StudioButton></div>
    </>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The Zoho Desk connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
