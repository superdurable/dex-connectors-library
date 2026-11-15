// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StudioHeader,
  StudioNotice,
  StudioSurface,
  type ConnectorConnectionView,
} from "@superdurable/dex-connectors-react";

export interface TrelloSetupViewProps { connection: ConnectorConnectionView; }

/** TrelloSetupView describes the API key and token connection; neither secret ever reaches this frame. */
export function TrelloSetupView({connection}: TrelloSetupViewProps) {
  return <StudioSurface label="Trello connection">
    <StudioHeader description="Connect one Trello member with an API key and a user token. Each Flow chooses its own board for the cardChanged Trigger and passes board, list, and card IDs to its operations." iconUrl="./icon.svg" title="Trello"/>
    {connection.state === "connected" && <StudioNotice tone="success">
      Connected. Flows act as the member who authorized the token, on every board that member can open.
    </StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      <p className="studio-muted">Paste the API key from https://trello.com/apps/admin and the token from Trello's authorize page into the connection form. Dex Web stores both as secrets and never sends them to this panel.</p>
    </>}
    {(connection.state === "revoked" || connection.state === "expired") && <StudioNotice tone="attention">
      Trello no longer accepts this token. Authorize the app again and paste the new token into the connection form; revoked tokens are listed under Applications at https://trello.com/u/your-username/account.
    </StudioNotice>}
    {(connection.state === "error" || connection.state === "broker_unavailable") &&
      <StudioNotice tone="error">{connection.detail || "The Trello connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
