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
import { appOnlyAuthMethodID } from "./provider.js";

export interface CalendarSetupViewProps {
  connection: ConnectorConnectionView;
  /** authMethodIds lists the connection's auth methods; an empty list means the host did not report them. */
  authMethodIds: readonly string[];
  onConnect(): void;
  onReconnect(): void;
}

const reconnectReasons: Partial<Record<ConnectorConnectionState, string>> = {
  expired: "Connection expired",
  revoked: "Connection revoked",
  insufficient_scope: "Additional permission required",
};

export function CalendarSetupView({connection, authMethodIds, onConnect, onReconnect}: CalendarSetupViewProps) {
  const reconnectReason = reconnectReasons[connection.state];
  const isAppOnly = authMethodIds.includes(appOnlyAuthMethodID);
  return <StudioSurface label="Microsoft Outlook Calendar connection">
    <StudioHeader description="Authorize one work or school user, or one app-only mailbox. Each Flow Step picks its own calendar." iconUrl="./icon.svg" title="Microsoft Outlook Calendar"/>
    {connection.state === "connected" && <StudioNotice tone="success">
      Connected{connection.accountEmail ? <> as <strong>{connection.accountEmail}</strong></> : ""}.
    </StudioNotice>}
    {connection.state === "not_configured" && isAppOnly &&
      <StudioNotice tone="info">Save the client ID, client secret, tenant ID, and mailbox in the connection form and leave access_token blank. The application requests a token on its first Microsoft Graph call; the calendar picker works after that.</StudioNotice>}
    {connection.state === "not_configured" && !isAppOnly && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onConnect} variant="primary">Connect Microsoft Outlook Calendar</StudioButton></div>
    </>}
    {reconnectReason && isAppOnly &&
      <StudioNotice tone="attention">{reconnectReason}. Check the client secret, its expiry, and the app's Exchange role assignment or Calendars.ReadWrite application permission.</StudioNotice>}
    {reconnectReason && !isAppOnly && <>
      <StudioNotice tone="attention">{reconnectReason}. Sign in to Microsoft again and accept every requested permission.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onReconnect} variant="primary">Reconnect Microsoft Outlook Calendar</StudioButton></div>
    </>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The Microsoft Outlook Calendar connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
