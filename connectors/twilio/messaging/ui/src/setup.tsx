// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StudioHeader,
  StudioNotice,
  StudioSurface,
  type ConnectorConnectionView,
} from "@superdurable/dex-connectors-react";

export interface TwilioSetupViewProps { connection: ConnectorConnectionView; }

/** TwilioSetupView explains the host form; Twilio credentials never reach this frame. */
export function TwilioSetupView({connection}: TwilioSetupViewProps) {
  return <StudioSurface label="Twilio Messaging connection">
    <StudioHeader
      description="Enter the Account SID and an Auth Token or API key in the form above. Each Flow configures its own Trigger bindings, such as the Twilio number whose inbound messages start it."
      iconUrl="./icon.svg" title="Twilio Messaging"/>
    {connection.state === "connected" && <StudioNotice tone="success">Connected.</StudioNotice>}
    {connection.state === "not_configured" && <StudioNotice tone="info">Not connected yet.</StudioNotice>}
    {(connection.state === "revoked" || connection.state === "expired") &&
      <StudioNotice tone="attention">Twilio rejected these credentials. Rotate the Auth Token or create a new API key in the Twilio Console, then save it above.</StudioNotice>}
    {(connection.state === "error" || connection.state === "broker_unavailable") &&
      <StudioNotice tone="error">{connection.detail || "The Twilio connection is not available."}</StudioNotice>}
  </StudioSurface>;
}
