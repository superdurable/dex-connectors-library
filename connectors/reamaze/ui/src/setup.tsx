// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioHeader, StudioNotice, StudioSurface, type ConnectorConnectionView } from "@superdurable/dex-connectors-react";

export interface ReamazeSetupViewProps {
  connection: ConnectorConnectionView;
}

export function ReamazeSetupView({connection}: ReamazeSetupViewProps) {
  return <StudioSurface label="Re:amaze connection">
    <StudioHeader
      description="Enter the brand, the staff user's login email, and that user's API token in the form above. Each Flow chooses the channel whose new conversations it receives below."
      iconUrl="./icon.svg"
      title="Re:amaze"
    />
    {connection.state === "connected"
      ? <StudioNotice tone="success">Connected.</StudioNotice>
      : <StudioNotice tone="info">{connection.detail || "Not connected yet."}</StudioNotice>}
  </StudioSurface>;
}
