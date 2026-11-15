// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioHeader, StudioNotice, StudioSurface, type ConnectorConnectionView } from "@superdurable/dex-connectors-react";

export interface BambooHRSetupViewProps {
  connection: ConnectorConnectionView;
}

export function BambooHRSetupView({connection}: BambooHRSetupViewProps) {
  return <StudioSurface label="BambooHR connection">
    <StudioHeader
      description="Enter the company domain and the BambooHR API key in the form above. Each Flow chooses below which employee changes its employeeChanged binding delivers."
      iconUrl="./icon.svg"
      title="BambooHR"
    />
    {connection.state === "connected"
      ? <StudioNotice tone="success">Connected.</StudioNotice>
      : <StudioNotice tone="info">{connection.detail || "Not connected yet."}</StudioNotice>}
  </StudioSurface>;
}
