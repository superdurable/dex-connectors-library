// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StudioHeader,
  StudioNotice,
  StudioSurface,
  type ConnectorConnectionView,
} from "@superdurable/dex-connectors-react";

export interface AsanaSetupViewProps { connection: ConnectorConnectionView; }

/** AsanaSetupView describes the personal access token connection; the token itself never reaches this frame. */
export function AsanaSetupView({connection}: AsanaSetupViewProps) {
  return <StudioSurface label="Asana connection">
    <StudioHeader description="Connect one Asana user with a personal access token. Each Flow Step picks its own workspace, project, and section." iconUrl="./icon.svg" title="Asana"/>
    {connection.state === "connected" && <StudioNotice tone="success">
      Connected with a personal access token. Tasks and comments are created as the token's Asana user.
    </StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      <p className="studio-muted">Paste the personal access token into the connection form. Dex Web stores it as a secret and never sends it to this panel.</p>
    </>}
    {(connection.state === "revoked" || connection.state === "expired") && <StudioNotice tone="attention">
      Asana no longer accepts this token. Create a new personal access token at https://app.asana.com/0/my-apps and paste it into the connection form.
    </StudioNotice>}
    {(connection.state === "error" || connection.state === "broker_unavailable") &&
      <StudioNotice tone="error">{connection.detail || "The Asana connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
