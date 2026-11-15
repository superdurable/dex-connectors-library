// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioHeader, StudioNotice, StudioSurface, type ConnectorConnectionView } from "@superdurable/dex-connectors-react";

export interface MailchimpSetupViewProps {
  connection: ConnectorConnectionView;
}

export function MailchimpSetupView({connection}: MailchimpSetupViewProps) {
  return <StudioSurface label="Mailchimp connection">
    <StudioHeader
      description="Paste the Mailchimp API key, including its data-center suffix such as -us6, in the form above. Each Flow enters the audience it watches below."
      iconUrl="./icon.svg"
      title="Mailchimp"
    />
    {connection.state === "connected"
      ? <StudioNotice tone="success">Connected.</StudioNotice>
      : <StudioNotice tone="info">{connection.detail || "Not connected yet."}</StudioNotice>}
  </StudioSurface>;
}
