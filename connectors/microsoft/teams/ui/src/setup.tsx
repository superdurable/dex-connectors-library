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

export interface TeamsSetupViewProps { connection: ConnectorConnectionView; onConnect(): void; onReconnect(): void; }

const reconnectReasons: Partial<Record<ConnectorConnectionState, string>> = {
  expired: "Connection expired",
  revoked: "Connection revoked",
  insufficient_scope: "Additional permission required",
};

const adminConsentNotice = "Reading thread replies needs ChannelMessage.Read.All, which a Microsoft Entra administrator must consent to for the organization. Without that consent Microsoft shows Need admin approval and the connection cannot complete.";

export function TeamsSetupView({connection, onConnect, onReconnect}: TeamsSetupViewProps) {
  const reconnectReason = reconnectReasons[connection.state];
  return <StudioSurface label="Microsoft Teams connection">
    <StudioHeader description="Authorize one Microsoft work or school account. Every message is posted as that account; each Flow Step picks its own team, channel, or chat." iconUrl="./icon.svg" title="Microsoft Teams"/>
    {connection.state === "connected" && <StudioNotice tone="success">
      Connected{connection.accountEmail ? <> as <strong>{connection.accountEmail}</strong></> : ""}. Messages show this account as the sender.
    </StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet. {adminConsentNotice}</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onConnect} variant="primary">Connect Microsoft Teams</StudioButton></div>
    </>}
    {reconnectReason && <>
      <StudioNotice tone="attention">{reconnectReason}. Sign in again with the same account and accept every requested permission. {adminConsentNotice}</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onReconnect} variant="primary">Reconnect Microsoft Teams</StudioButton></div>
    </>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The Microsoft Teams connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
