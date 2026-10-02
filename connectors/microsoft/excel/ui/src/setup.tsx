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

export interface ExcelSetupViewProps { connection: ConnectorConnectionView; onConnect(): void; onReconnect(): void; }

const reconnectReasons: Partial<Record<ConnectorConnectionState, string>> = {
  expired: "Connection expired",
  revoked: "Connection revoked",
  insufficient_scope: "Additional permission required",
};

export function ExcelSetupView({connection, onConnect, onReconnect}: ExcelSetupViewProps) {
  const reconnectReason = reconnectReasons[connection.state];
  return <StudioSurface label="Microsoft Excel connection">
    <StudioHeader description="Authorize a Microsoft work or school account. Each Flow chooses its own workbooks, tables, and worksheets." iconUrl="./icon.svg" title="Microsoft Excel"/>
    {connection.state === "connected" && <StudioNotice tone="success">
      Connected{connection.accountEmail ? <> as <strong>{connection.accountEmail}</strong></> : ""}.
    </StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      <p className="studio-muted">Dex requests Files.ReadWrite.All, to read and write the workbooks you can open in OneDrive and SharePoint, and offline_access, so it can refresh access without asking again.</p>
      <div className="studio-actions"><StudioButton onClick={onConnect} variant="primary">Connect Microsoft Excel</StudioButton></div>
    </>}
    {reconnectReason && <>
      <StudioNotice tone="attention">{reconnectReason}. Authorize Microsoft Excel again to keep using it.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onReconnect} variant="primary">Reconnect Microsoft Excel</StudioButton></div>
    </>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The Microsoft Excel connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
