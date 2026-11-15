// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StudioButton,
  StudioField,
  StudioHeader,
  StudioNotice,
  StudioSurface,
  type ConnectorStudioConfigurationUnitTarget,
} from "@superdurable/dex-connectors-react";
import { useState } from "react";
import { channelError, savedChannel } from "./channel.js";

export interface ReamazeUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  busy?: boolean;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function ReamazeConfigurationUnit(props: ReamazeUnitProps) {
  switch (props.target.unitId) {
    case "channelSlugEntry": return <ChannelSlugEntryUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported Re:amaze configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function ChannelSlugEntryUnit({target, busy, onSave}: ReamazeUnitProps) {
  const [channel, setChannel] = useState(savedChannel(target.value.channel));
  const error = channelError(channel, target.required);
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    <StudioField
      hint="Find the slug in the slug field of GET https://{brand}.reamaze.io/api/v1/channels, or under Settings > Channels in Re:amaze. Leave it blank to receive conversations from every channel of the brand. The slug is not secret and is saved in the binding's project configuration."
      label="Channel slug"
    >
      <input
        aria-invalid={error ? true : undefined}
        autoComplete="off"
        onChange={(change) => setChannel(change.target.value.trim())}
        placeholder="support"
        spellCheck={false}
        type="text"
        value={channel}
      />
    </StudioField>
    {error && <StudioNotice tone="error">{error}</StudioNotice>}
    <SaveAction disabled={busy || error !== undefined} onSave={() => onSave(channel === "" ? {} : {channel})}/>
  </StudioSurface>;
}

function SaveAction({disabled, onSave}: {disabled?: boolean; onSave(): Promise<unknown> | void}) {
  const [saveState, setSaveState] = useState<{status: "idle" | "saved"} | {status: "failed"; message: string}>({status: "idle"});
  const save = () => {
    Promise.resolve(onSave()).then(
      () => setSaveState({status: "saved"}),
      (error: unknown) => setSaveState({status: "failed", message: error instanceof Error ? error.message : "Unknown error"}),
    );
  };
  return <>
    <div className="studio-actions"><StudioButton disabled={disabled} onClick={save} variant="primary">Save</StudioButton></div>
    {saveState.status === "saved" && <StudioNotice tone="success">Saved. Restart the application to use it.</StudioNotice>}
    {saveState.status === "failed" && <StudioNotice tone="error">The configuration could not be saved: {saveState.message}</StudioNotice>}
  </>;
}
