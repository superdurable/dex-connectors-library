// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioButton, StudioField, StudioHeader, StudioNotice, StudioSurface, type ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { useState } from "react";
import { topicARNFromInput } from "./topic.js";

export interface SNSUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  busy?: boolean;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

/** SNSConfigurationUnit renders the topic selector for notificationReceived. */
export function SNSConfigurationUnit({target, busy, onSave}: SNSUnitProps) {
  if (target.unitId !== "topicARNInput") return <StudioNotice tone="error">Unsupported SNS configuration unit: {target.unitId}</StudioNotice>;
  return <TopicARNInputUnit target={target} busy={busy} onSave={onSave}/>;
}

function TopicARNInputUnit({target, busy, onSave}: SNSUnitProps) {
  const saved = typeof target.value.topicARN === "string" ? target.value.topicARN : "";
  const [input, setInput] = useState(saved);
  const [saveState, setSaveState] = useState<"idle" | "saved" | "failed">("idle");
  const topicARN = topicARNFromInput(input);
  function save() {
    if (!topicARN) return;
    Promise.resolve(onSave({topicARN})).then(() => setSaveState("saved"), () => setSaveState("failed"));
  }
  return <StudioSurface label={target.label}>
    <StudioHeader title={target.label} description={target.description}/>
    <StudioField label={target.label} hint="Open https://console.aws.amazon.com/sns > Topics, select the topic, then copy Topic ARN. It must match the connection Region.">
      <input type="text" value={input} onChange={(event) => setInput(event.target.value)} placeholder="arn:aws:sns:us-east-1:123456789012:example-topic"/>
    </StudioField>
    {input.trim() !== "" && !topicARN && <StudioNotice tone="error">Enter a complete SNS topic ARN with a 12-digit AWS account ID.</StudioNotice>}
    <div className="studio-actions"><StudioButton variant="primary" disabled={busy || !topicARN} onClick={save}>Save</StudioButton></div>
    {saveState === "saved" && <StudioNotice tone="success">Saved. Restart the application to use this topic.</StudioNotice>}
    {saveState === "failed" && <StudioNotice tone="error">The topic could not be saved.</StudioNotice>}
  </StudioSurface>;
}
