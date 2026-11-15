// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioHeader, StudioNotice, StudioSurface, type ConnectorConnectionView } from "@superdurable/dex-connectors-react";

export interface MondaySetupViewProps {
  connection: ConnectorConnectionView;
}

export function MondaySetupView({connection}: MondaySetupViewProps) {
  return <StudioSurface label="monday.com connection">
    <StudioHeader
      description="Enter the personal API token, or connect the monday.com OAuth app, in the form above. Each itemChanged Trigger binding enters the board it watches below."
      iconUrl="./icon.svg"
      title="monday.com"
    />
    {connection.state === "connected"
      ? <StudioNotice tone="success">Connected.</StudioNotice>
      : <StudioNotice tone="info">{connection.detail || "Not connected yet."}</StudioNotice>}
  </StudioSurface>;
}
