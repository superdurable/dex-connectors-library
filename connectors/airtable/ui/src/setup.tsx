// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StudioHeader,
  StudioNotice,
  StudioSurface,
  type ConnectorConnectionView,
} from "@superdurable/dex-connectors-react";

export interface AirtableSetupViewProps { connection: ConnectorConnectionView; }

export function AirtableSetupView({connection}: AirtableSetupViewProps) {
  return <StudioSurface label="Airtable connection">
    <StudioHeader description="Connect Airtable with a personal access token. Each Flow chooses its own bases and tables." iconUrl="./icon.svg" title="Airtable"/>
    {connection.state === "connected" && <StudioNotice tone="success">Connected with a personal access token.</StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      <p className="studio-muted">Paste a personal access token with the data.records:read, data.records:write, and schema.bases:read scopes into the connection form. Dex Web stores it as a secret and never sends it to this panel.</p>
    </>}
    {(connection.state === "expired" || connection.state === "revoked" || connection.state === "insufficient_scope") &&
      <StudioNotice tone="attention">Airtable no longer accepts this token. Paste a current personal access token, with access to the Flows' bases, into the connection form.</StudioNotice>}
    {(connection.state === "error" || connection.state === "broker_unavailable" || connection.state === "authorization_pending") &&
      <StudioNotice tone={connection.state === "authorization_pending" ? "info" : "error"}>{connection.detail || "The Airtable connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
