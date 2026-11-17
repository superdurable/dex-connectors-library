// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioHeader, StudioNotice, StudioSurface, type ConnectorConnectionView } from "@superdurable/dex-connectors-react";

export interface SNSSetupViewProps { connection: ConnectorConnectionView; }

/** SNSSetupView explains connection state without receiving credentials. */
export function SNSSetupView({connection}: SNSSetupViewProps) {
  return <StudioSurface label="Amazon SNS connection">
    <StudioHeader title="Amazon SNS" iconUrl="./icon.svg" description="Use the AWS access key configured in Dex Web for the selected Region. Notification bindings each select one SNS topic."/>
    {connection.state === "connected" && <StudioNotice tone="success">Connected. Select a topic ARN for each notification binding.</StudioNotice>}
    {connection.state === "not_configured" && <StudioNotice tone="info">Open https://console.aws.amazon.com/iam &gt; Users &gt; Security credentials &gt; Access keys to create a key, then enter it in the Dex Web connection form. This panel never receives the key or secret.</StudioNotice>}
    {(connection.state === "revoked" || connection.state === "expired") && <StudioNotice tone="attention">The AWS credentials need replacement in the Dex Web connection form.</StudioNotice>}
    {(connection.state === "error" || connection.state === "broker_unavailable") && <StudioNotice tone="error">{connection.detail || "The SNS connection is unavailable."}</StudioNotice>}
  </StudioSurface>;
}
