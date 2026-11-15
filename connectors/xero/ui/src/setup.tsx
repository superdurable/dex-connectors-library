// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StudioHeader,
  StudioNotice,
  StudioSurface,
  type ConnectorConnectionView,
} from "@superdurable/dex-connectors-react";

export interface XeroSetupViewProps { connection: ConnectorConnectionView; }

/** XeroSetupView describes the connection; the client secret and tokens never reach this frame. */
export function XeroSetupView({connection}: XeroSetupViewProps) {
  return <StudioSurface label="Xero connection">
    <StudioHeader description="Connect one Xero organisation with a Custom Connection or a Xero web app. Each Flow chooses which invoices start its invoiceChanged Trigger." iconUrl="./icon.svg" title="Xero"/>
    {connection.state === "connected" && <StudioNotice tone="success">
      Connected. The invoiceChanged Trigger polls this organisation's invoices at the connection's poll interval.
    </StudioNotice>}
    {connection.state === "not_configured" && <>
      <StudioNotice tone="info">Not connected yet.</StudioNotice>
      <p className="studio-muted">Follow the steps in the connection form: copy the Client ID and a generated secret from https://developer.xero.com/app/manage. Dex Web stores the secret and tokens and never sends them to this panel.</p>
    </>}
    {(connection.state === "revoked" || connection.state === "expired") && <StudioNotice tone="attention">
      Xero no longer accepts this connection. Generate a new secret at https://developer.xero.com/app/manage, or authorize again, then save the connection form.
    </StudioNotice>}
    {(connection.state === "error" || connection.state === "broker_unavailable") &&
      <StudioNotice tone="error">{connection.detail || "The Xero connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
