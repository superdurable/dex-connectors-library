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

export interface OutlookMailSetupViewProps { connection: ConnectorConnectionView; onConnect(): void; onReconnect(): void; }

const reconnectReasons: Partial<Record<ConnectorConnectionState, string>> = {
  expired: "Connection expired",
  revoked: "Connection revoked",
  insufficient_scope: "Additional permission required",
};

export function OutlookMailSetupView({connection, onConnect, onReconnect}: OutlookMailSetupViewProps) {
  if ((connection.authMethodIds ?? []).includes("app-only")) return <AppOnlySetupView connection={connection}/>;
  const reconnectReason = reconnectReasons[connection.state];
  return <StudioSurface label="Microsoft Outlook Mail connection">
    <StudioHeader
      description="Authorize the Microsoft 365 work or school account whose mailbox the Flows use. Each Flow Step picks its own folders."
      iconUrl="./icon.svg"
      title="Microsoft Outlook Mail"
    />
    {connection.state === "connected" && <StudioNotice tone="success">
      Connected{connection.accountEmail ? <> as <strong>{connection.accountEmail}</strong></> : ""}.
    </StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet. Register the Redirect URI shown in the form on a multitenant Entra app first.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onConnect} variant="primary">Connect Microsoft Outlook</StudioButton></div>
    </>}
    {reconnectReason && <>
      <StudioNotice tone="attention">{reconnectReason}. Authorize again and accept Mail.ReadWrite, Mail.Send, and offline access.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onReconnect} variant="primary">Reconnect Microsoft Outlook</StudioButton></div>
    </>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The Outlook Mail connection is not available."}</StudioNotice>}
  </StudioSurface>;
}

function AppOnlySetupView({connection}: {connection: ConnectorConnectionView}) {
  return <StudioSurface label="Microsoft Outlook Mail connection">
    <StudioHeader
      description="Enter the tenant ID, client ID, client secret, and mailbox in the form above, and leave access_token blank. The application requests app-only tokens itself and renews them; limit the app to the mailbox with Exchange RBAC for Applications."
      iconUrl="./icon.svg"
      title="Microsoft Outlook Mail"
    />
    {connection.state === "connected"
      ? <StudioNotice tone="success">Saved. Folders are listed with the access token the application stores on its first Outlook call.</StudioNotice>
      : <StudioNotice tone="info">{connection.detail || "Not saved yet."}</StudioNotice>}
  </StudioSurface>;
}
