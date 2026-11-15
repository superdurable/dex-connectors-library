// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StudioHeader,
  StudioNotice,
  StudioSurface,
  type ConnectorConnectionView,
} from "@superdurable/dex-connectors-react";

export interface NotionSetupViewProps { connection: ConnectorConnectionView; }

/** NotionSetupView describes the internal connection API token; the token never reaches this frame. */
export function NotionSetupView({connection}: NotionSetupViewProps) {
  return <StudioSurface label="Notion connection">
    <StudioHeader description="Connect one Notion workspace with an internal connection's API token. Each Flow picks its own databases, and the databaseRowChanged Trigger picks the data source it watches." iconUrl="./icon.svg" title="Notion"/>
    {connection.state === "connected" && <StudioNotice tone="success">
      Connected with an API token. Databases shared with the connection resolve live from Notion.
    </StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      <p className="studio-muted">Paste the API token from https://app.notion.com/developers/connections into the connection form. Dex Web stores it as a secret and never sends it to this panel.</p>
    </>}
    {(connection.state === "revoked" || connection.state === "expired") && <StudioNotice tone="attention">
      Notion no longer accepts this token. Open the connection at https://app.notion.com/developers/connections, choose Refresh on its Configuration tab, and paste the new API token into the connection form.
    </StudioNotice>}
    {(connection.state === "error" || connection.state === "broker_unavailable") &&
      <StudioNotice tone="error">{connection.detail || "The Notion connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
