// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioHeader, StudioNotice, StudioSurface, type ConnectorConnectionView } from "@superdurable/dex-connectors-react";

export interface HelpScoutSetupViewProps { connection: ConnectorConnectionView; }

export function HelpScoutSetupView({connection}: HelpScoutSetupViewProps) {
  return <StudioSurface label="Help Scout connection">
    <StudioHeader
      description="Enter your Help Scout app's App ID and App Secret, and the webhook secret, in the form above; leave access_token blank. The application obtains a two-day access token from them and renews it. Each Flow configures its own operations and Triggers."
      iconUrl="./icon.svg"
      title="Help Scout"
    />
    {connection.state === "connected"
      ? <StudioNotice tone="success">Connected. Inboxes are listed with the access token the application stored.</StudioNotice>
      : <StudioNotice tone="info">{connection.detail || "Not connected yet."}</StudioNotice>}
  </StudioSurface>;
}
