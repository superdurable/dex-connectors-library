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

export const privateAppTokenAuthMethodID = "private-app-token";
export const oauthAuthMethodID = "hubspot-oauth";

export interface HubSpotSetupViewProps {
  connection: ConnectorConnectionView;
  onConnect(): void;
  onReconnect(): void;
}

const reconnectReasons: Partial<Record<ConnectorConnectionState, string>> = {
  expired: "Connection expired",
  revoked: "Connection revoked",
  insufficient_scope: "Additional permission required",
};

export function HubSpotSetupView({connection, onConnect, onReconnect}: HubSpotSetupViewProps) {
  const authMethodIds = connection.authMethodIds ?? [];
  const isOAuth = authMethodIds.includes(oauthAuthMethodID);
  const isPrivateAppToken = authMethodIds.includes(privateAppTokenAuthMethodID);
  const reconnectReason = reconnectReasons[connection.state];
  return <StudioSurface label="HubSpot connection">
    <StudioHeader description="Connect one HubSpot account. Each Flow chooses its own owner, pipeline, and stage values." iconUrl="./icon.svg" title="HubSpot"/>
    {connection.state === "connected" && <StudioNotice tone="success">
      {isOAuth ? "Connected with HubSpot OAuth. Dex refreshes the access token automatically." : isPrivateAppToken ? "Connected with a private app access token." : "Connected."}
    </StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      {isOAuth && <div className="studio-actions"><StudioButton onClick={onConnect} variant="primary">Connect HubSpot</StudioButton></div>}
      {isPrivateAppToken && <p className="studio-muted">Paste the private app access token into the connection form. Dex Web stores it as a secret and never sends it to this panel.</p>}
      {!isOAuth && !isPrivateAppToken && <p className="studio-muted">Choose Private app access token for one HubSpot account, or HubSpot OAuth for an app installed through consent, then follow its steps.</p>}
    </>}
    {reconnectReason && <>
      <StudioNotice tone="attention">{reconnectReason}. {isOAuth ? "Authorize HubSpot again to keep using it." : "Paste a current private app access token into the connection form."}</StudioNotice>
      {isOAuth && <div className="studio-actions"><StudioButton onClick={onReconnect} variant="primary">Reconnect HubSpot</StudioButton></div>}
    </>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The HubSpot connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
