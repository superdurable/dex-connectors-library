// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioButton, StudioHeader, StudioNotice, StudioSurface, type ConnectorConnectionView } from "@superdurable/dex-connectors-react";

export interface SlackSetupViewProps {
  connection: ConnectorConnectionView;
  onConnect(): void;
  onReconnect(): void;
}

export function SlackSetupView({connection, onConnect, onReconnect}: SlackSetupViewProps) {
  const needsReconnect = connection.state === "expired" || connection.state === "revoked" || connection.state === "insufficient_scope";
  return <StudioSurface label="Slack connection">
    <StudioHeader description="Authorize a workspace connection. Each Flow configures its operations and Triggers separately." iconUrl="./icon.svg" title="Slack"/>
    {connection.state === "connected" && <StudioNotice tone="success">Connection ready. Dex displays each Flow's reusable Slack configuration units below this connection.</StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onConnect} variant="primary">Connect Slack</StudioButton></div>
    </>}
    {needsReconnect && <>
      <StudioNotice tone="attention">{connection.detail || "This connection needs to be authorized again."}</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={onReconnect} variant="primary">Reconnect Slack</StudioButton></div>
    </>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The Slack connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
