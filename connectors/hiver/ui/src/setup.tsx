// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioHeader, StudioNotice, StudioSurface, type ConnectorConnectionView } from "@superdurable/dex-connectors-react";

export interface HiverSetupViewProps { connection: ConnectorConnectionView; }

export function HiverSetupView({connection}: HiverSetupViewProps) {
  return <StudioSurface label="Hiver connection">
    <StudioHeader
      description="Paste the Hiver admin's API key into api_key in the form above and save; Dex stores it as a secret and never shows it here. Leave the other settings blank unless Hiver raised your account's request limit. Each Flow configures its own operations and Triggers, such as the shared inbox a conversationReceived binding watches."
      iconUrl="./icon.svg"
      title="Hiver"
    />
    {connection.state === "connected"
      ? <StudioNotice tone="success">Connected. Shared inboxes are listed with the saved API key.</StudioNotice>
      : <StudioNotice tone="info">{connection.detail || "Not connected yet."}</StudioNotice>}
  </StudioSurface>;
}
