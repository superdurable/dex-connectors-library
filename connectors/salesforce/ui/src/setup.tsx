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

export const jwtBearerAuthMethodID = "salesforce-jwt-bearer";

export interface SalesforceSetupViewProps {
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

export function SalesforceSetupView({connection, authMethodIds, onConnect, onReconnect}: SalesforceSetupViewProps) {
  const reconnectReason = reconnectReasons[connection.state];
  const isJWTBearer = authMethodIds.includes(jwtBearerAuthMethodID);
  return <StudioSurface label="Salesforce connection">
    <StudioHeader description="Authorize an org user. Each Flow chooses its own objects and fields." iconUrl="./icon.svg" title="Salesforce"/>
    {connection.state === "connected" && <StudioNotice tone="success">
      Connected{connection.accountEmail ? <> as <strong>{connection.accountEmail}</strong></> : ""}.
    </StudioNotice>}
    {connection.state === "not_configured" && isJWTBearer &&
      <StudioNotice tone="info">Save the consumer key, username, private key, and login environment in the connection form; Dex mints each session itself.</StudioNotice>}
    {connection.state === "not_configured" && !isJWTBearer && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onConnect} variant="primary">Connect Salesforce</StudioButton></div>
    </>}
    {reconnectReason && isJWTBearer &&
      <StudioNotice tone="attention">{reconnectReason}. Check that the integration user is still pre-authorized for the app and that the saved private key matches its certificate.</StudioNotice>}
    {reconnectReason && !isJWTBearer && <>
      <StudioNotice tone="attention">{reconnectReason}. Authorize Salesforce again to keep using it.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onReconnect} variant="primary">Reconnect Salesforce</StudioButton></div>
    </>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The Salesforce connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
