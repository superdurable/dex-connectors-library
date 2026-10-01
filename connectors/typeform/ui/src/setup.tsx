// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StudioHeader,
  StudioNotice,
  StudioSurface,
  type ConnectorConnectionView,
} from "@superdurable/dex-connectors-react";

export interface TypeformSetupViewProps { connection: ConnectorConnectionView; }

/** TypeformSetupView describes the personal access token connection; the token never reaches this frame. */
export function TypeformSetupView({connection}: TypeformSetupViewProps) {
  return <StudioSurface label="Typeform connection">
    <StudioHeader description="Connect one Typeform account with a personal access token. Each Flow picks its own form for the responseSubmitted Trigger and its operations." iconUrl="./icon.svg" title="Typeform"/>
    {connection.state === "connected" && <StudioNotice tone="success">
      Connected with a personal access token. Forms are listed live from Typeform.
    </StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      <p className="studio-muted">Paste the personal access token, and the webhook secret if this connection receives submissions, into the connection form. Dex Web stores both as secrets and never sends them to this panel.</p>
    </>}
    {(connection.state === "revoked" || connection.state === "expired") && <StudioNotice tone="attention">
      Typeform no longer accepts this token. Generate a new personal access token at https://admin.typeform.com/user/tokens and paste it into the connection form.
    </StudioNotice>}
    {(connection.state === "error" || connection.state === "broker_unavailable") &&
      <StudioNotice tone="error">{connection.detail || "The Typeform connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
