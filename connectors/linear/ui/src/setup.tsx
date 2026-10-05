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

export const oauthAuthMethodID = "linear-oauth";

export interface LinearSetupViewProps { connection: ConnectorConnectionView; onConnect(): void; onReconnect(): void; }

const reconnectReasons: Partial<Record<ConnectorConnectionState, string>> = {
  expired: "Connection expired",
  revoked: "Connection revoked",
  insufficient_scope: "Additional permission required",
};

export function LinearSetupView({connection, onConnect, onReconnect}: LinearSetupViewProps) {
  const isOAuth = (connection.authMethodIds ?? []).includes(oauthAuthMethodID);
  const reconnectReason = reconnectReasons[connection.state];
  return <StudioSurface label="Linear connection">
    <StudioHeader description="Paste a personal API key in the form above, or authorize a Linear OAuth application. Each Flow configures its own operations and Triggers." iconUrl="./icon.svg" title="Linear"/>
    {connection.state === "connected" && <StudioNotice tone="success">Connected. {isOAuth ? "Teams are listed live from Linear." : "Team pickers accept a team UUID for a personal API key."}</StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      {isOAuth && <div className="studio-actions"><StudioButton onClick={onConnect} variant="primary">Connect Linear</StudioButton></div>}
    </>}
    {reconnectReason && <>
      <StudioNotice tone="attention">{reconnectReason}. {isOAuth ? "Authorize Linear again to keep using it." : "Create a new personal API key and save it above."}</StudioNotice>
      {isOAuth && <div className="studio-actions"><StudioButton onClick={onReconnect} variant="primary">Reconnect Linear</StudioButton></div>}
    </>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The Linear connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
