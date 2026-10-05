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
import { apiTokenAuthMethodID, oauthAuthMethodID } from "./provider.js";

export interface PipedriveSetupViewProps {
  connection: ConnectorConnectionView;
  onConnect(): void;
  onReconnect(): void;
}

const reconnectReasons: Partial<Record<ConnectorConnectionState, string>> = {
  expired: "Connection expired",
  revoked: "Connection revoked",
  insufficient_scope: "Additional permission required",
};

export function PipedriveSetupView({connection, onConnect, onReconnect}: PipedriveSetupViewProps) {
  const authMethodIds = connection.authMethodIds ?? [];
  const isOAuth = authMethodIds.includes(oauthAuthMethodID);
  const isAPIToken = authMethodIds.includes(apiTokenAuthMethodID);
  const reconnectReason = reconnectReasons[connection.state];
  return <StudioSurface label="Pipedrive connection">
    <StudioHeader description="Connect one Pipedrive company. Each Flow chooses its own owner, pipeline, stage, and custom field values." iconUrl="./icon.svg" title="Pipedrive"/>
    {connection.state === "connected" && <StudioNotice tone="success">
      {isOAuth
        ? "Connected with a Pipedrive OAuth app. Dex refreshes the hourly access token automatically and sends requests to your company's API domain."
        : isAPIToken ? "Connected with a Personal API token." : "Connected."}
    </StudioNotice>}
    {connection.state === "connected" && isOAuth && <p className="studio-muted">
      The owner, stage, and custom field pickers need a Personal API token connection; with this OAuth connection, enter those IDs by hand.
    </p>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      {isOAuth && <div className="studio-actions"><StudioButton onClick={onConnect} variant="primary">Connect Pipedrive</StudioButton></div>}
      {isAPIToken && <p className="studio-muted">Paste the Personal API token from Pipedrive Personal preferences &gt; API into the connection form. Dex Web stores it as a secret and never sends it to this panel.</p>}
      {!isOAuth && !isAPIToken && <p className="studio-muted">Choose Personal API token for one Pipedrive company, or Pipedrive OAuth app for an app installed through consent, then follow its steps.</p>}
    </>}
    {reconnectReason && <>
      <StudioNotice tone="attention">{reconnectReason}. {isOAuth ? "Authorize Pipedrive again to keep using it." : "Paste a current Personal API token into the connection form."}</StudioNotice>
      {isOAuth && <div className="studio-actions"><StudioButton onClick={onReconnect} variant="primary">Reconnect Pipedrive</StudioButton></div>}
    </>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The Pipedrive connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
