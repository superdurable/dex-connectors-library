// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioHeader, StudioNotice, StudioSurface, type ConnectorConnectionView } from "@superdurable/dex-connectors-react";

export interface IntercomSetupViewProps {
  connection: ConnectorConnectionView;
}

export function IntercomSetupView({connection}: IntercomSetupViewProps) {
  return <StudioSurface label="Intercom connection">
    <StudioHeader
      description="Enter the region, the app's access token, and, for webhooks, its client secret in the form above. Each Flow chooses its replying admin and webhook topics below."
      iconUrl="./icon.svg"
      title="Intercom"
    />
    {connection.state === "connected"
      ? <StudioNotice tone="success">Connected. Admins are listed live from the workspace.</StudioNotice>
      : <StudioNotice tone="info">{connection.detail || "Not connected yet."}</StudioNotice>}
  </StudioSurface>;
}
