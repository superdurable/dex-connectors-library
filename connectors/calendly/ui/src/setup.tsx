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

export const oauthAuthMethodID = "calendly-oauth";

export interface CalendlySetupViewProps { connection: ConnectorConnectionView; onConnect(): void; onReconnect(): void; }

const reconnectReasons: Partial<Record<ConnectorConnectionState, string>> = {
  expired: "Connection expired",
  revoked: "Connection revoked",
  insufficient_scope: "Additional permission required",
};

export function CalendlySetupView({connection, onConnect, onReconnect}: CalendlySetupViewProps) {
  const isOAuth = (connection.authMethodIds ?? []).includes(oauthAuthMethodID);
  const reconnectReason = reconnectReasons[connection.state];
  return <StudioSurface label="Calendly connection">
    <StudioHeader description="Paste a personal access token in the form above, or authorize a Calendly OAuth app. Each Flow configures its own operations and Triggers." iconUrl="./icon.svg" title="Calendly"/>
    {connection.state === "connected" && <StudioNotice tone="success">Connected. Event types are listed live from Calendly.</StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      {isOAuth && <div className="studio-actions"><StudioButton onClick={onConnect} variant="primary">Connect Calendly</StudioButton></div>}
    </>}
    {reconnectReason && <>
      <StudioNotice tone="attention">{reconnectReason}. {isOAuth ? "Authorize Calendly again to keep using it." : "Generate a new personal access token and save it above."}</StudioNotice>
      {isOAuth && <div className="studio-actions"><StudioButton onClick={onReconnect} variant="primary">Reconnect Calendly</StudioButton></div>}
    </>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The Calendly connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
