// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioHeader, StudioNotice, StudioSurface, type ConnectorConnectionView } from "@superdurable/dex-connectors-react";

export interface ClickUpSetupViewProps {
  connection: ConnectorConnectionView;
}

export function ClickUpSetupView({connection}: ClickUpSetupViewProps) {
  return <StudioSurface label="ClickUp connection">
    <StudioHeader
      description="Enter the personal API token and, for task webhooks, the webhook's signing secret in the form above. Each Flow chooses the webhook events it accepts below."
      iconUrl="./icon.svg"
      title="ClickUp"
    />
    {connection.state === "connected"
      ? <StudioNotice tone="success">Connected.</StudioNotice>
      : <StudioNotice tone="info">{connection.detail || "Not connected yet."}</StudioNotice>}
  </StudioSurface>;
}
