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

export interface CalendarSetupViewProps { connection: ConnectorConnectionView; onConnect(): void; onReconnect(): void; }

const reconnectReasons: Partial<Record<ConnectorConnectionState, string>> = {
  expired: "Connection expired",
  revoked: "Connection revoked",
  insufficient_scope: "Additional permission required",
};

export function CalendarSetupView({connection, onConnect, onReconnect}: CalendarSetupViewProps) {
  const reconnectReason = reconnectReasons[connection.state];
  return <StudioSurface label="Google Calendar connection">
    <StudioHeader description="Authorize an account. Each Flow Step picks its own calendar." iconUrl="./icon.svg" title="Google Calendar"/>
    {connection.state === "connected" && <StudioNotice tone="success">
      Connected{connection.accountEmail ? <> as <strong>{connection.accountEmail}</strong></> : ""}.
    </StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onConnect} variant="primary">Connect Google Calendar</StudioButton></div>
    </>}
    {reconnectReason && <>
      <StudioNotice tone="attention">{reconnectReason}. Authorize Google Calendar again and keep every requested scope checked.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onReconnect} variant="primary">Reconnect Google Calendar</StudioButton></div>
    </>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The Google Calendar connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
