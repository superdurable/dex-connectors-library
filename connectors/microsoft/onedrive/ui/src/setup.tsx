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

export const appOnlyAuthMethodID = "microsoft-app-only";

export interface OneDriveSetupViewProps { connection: ConnectorConnectionView; onConnect(): void; onReconnect(): void; }

const reconnectReasons: Partial<Record<ConnectorConnectionState, string>> = {
  expired: "Connection expired",
  revoked: "Connection revoked",
  insufficient_scope: "Additional permission required",
};

export function OneDriveSetupView({connection, onConnect, onReconnect}: OneDriveSetupViewProps) {
  const isAppOnly = connection.authMethodIds?.includes(appOnlyAuthMethodID) ?? false;
  const reconnectReason = reconnectReasons[connection.state];
  return <StudioSurface label="Microsoft OneDrive and SharePoint connection">
    <StudioHeader description="Authorize a work or school account. Each Flow chooses its own SharePoint site, drive, and folder." iconUrl="./icon.svg" title="Microsoft OneDrive and SharePoint"/>
    {isAppOnly && <StudioNotice tone="info">
      App-only connection: the application requests a Microsoft Graph token from the tenant ID, client ID, and client secret on its first call. The pickers work after that first token is stored, and every Step needs a drive ID.
    </StudioNotice>}
    {!isAppOnly && connection.state === "connected" && <StudioNotice tone="success">
      Connected{connection.accountEmail ? <> as <strong>{connection.accountEmail}</strong></> : ""}.
    </StudioNotice>}
    {!isAppOnly && connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onConnect} variant="primary">Connect Microsoft account</StudioButton></div>
    </>}
    {!isAppOnly && reconnectReason && <>
      <StudioNotice tone="attention">{reconnectReason}. Authorize the Microsoft account again to keep using it.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onReconnect} variant="primary">Reconnect Microsoft account</StudioButton></div>
    </>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The Microsoft connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
