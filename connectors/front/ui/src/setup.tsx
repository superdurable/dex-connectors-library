// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioHeader, StudioNotice, StudioSurface, type ConnectorConnectionView } from "@superdurable/dex-connectors-react";

/** FrontSetupViewProps carries the connection view; it never holds the API token. */
export interface FrontSetupViewProps {
  connection: ConnectorConnectionView;
}

/** FrontSetupView explains where the API token comes from and shows the connection state. */
export function FrontSetupView({connection}: FrontSetupViewProps) {
  return <StudioSurface label="Front connection">
    <StudioHeader
      description="Enter a Front API token in the form above; create it as a company admin at Settings > Developers > API Tokens. Each Flow chooses its inboxes, teammates, and tags below."
      iconUrl="./icon.svg"
      title="Front"
    />
    {connection.state === "connected"
      ? <StudioNotice tone="success">Connected. Inboxes, teammates, and tags are listed live with the API token.</StudioNotice>
      : <StudioNotice tone="info">{connection.detail || "Not connected yet."}</StudioNotice>}
  </StudioSurface>;
}
