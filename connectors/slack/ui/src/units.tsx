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
import { useState, type ReactNode } from "react";

export interface SlackChannel { id: string; name: string; isPrivate?: boolean; }
export interface SlackUser { id: string; displayName: string; imageUrl?: string; }

export interface SlackUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  channels: SlackChannel[];
  users: SlackUser[];
  busy?: boolean;
  loadError?: string;
  onLoadChannels(): void;
  onLoadUsers(): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function SlackConfigurationUnit(props: SlackUnitProps) {
  switch (props.target.unitId) {
    case "channelPicker": return <ChannelPickerUnit {...props}/>;
    case "memberPicker": return <MemberPickerUnit {...props}/>;
    case "textInput": return <TextInputUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported Slack configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function ChannelPickerUnit({target, channels, busy, loadError, onLoadChannels, onSave}: SlackUnitProps) {
  const [channelId, setChannelId] = useState(stringValue(target.value.channelId));
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadChannels}>Load joined channels</StudioButton>
      <span className="studio-muted">Only channels this app has joined are available.</span>
    </div>
    <StudioField label="Channel">
      <select onChange={(event) => setChannelId(event.target.value)} value={channelId}>
        <option value="">Select a channel</option>
        {channels.map((channel) => <option key={channel.id} value={channel.id}>#{channel.name}{channel.isPrivate ? " (private)" : ""} · {channel.id}</option>)}
      </select>
    </StudioField>
    <StudioField hint="Use a channel ID when the channel is not listed." label="Channel ID fallback">
      <input onChange={(event) => setChannelId(event.target.value.trim())} placeholder="C0123456789" type="text" value={channelId}/>
    </StudioField>
    <SaveAction disabled={busy || (target.required && channelId.length === 0)} onSave={() => onSave({channelId})}/>
  </UnitFrame>;
}

function MemberPickerUnit({target, users, busy, loadError, onLoadUsers, onSave}: SlackUnitProps) {
  const [memberIds, setMemberIds] = useState(stringArray(target.value.memberIds));
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions"><StudioButton disabled={busy} onClick={onLoadUsers}>Load members</StudioButton></div>
    {users.length > 0 && <fieldset aria-label="Members" className="studio-options">
      {users.map((user) => <label className="studio-option" key={user.id}>
        <input checked={memberIds.includes(user.id)} onChange={(event) => setMemberIds(event.target.checked ? [...memberIds, user.id] : memberIds.filter((id) => id !== user.id))} type="checkbox"/>
        <span className="studio-option-label">{user.imageUrl && <img alt="" src={user.imageUrl} style={{width: 20, height: 20, borderRadius: 5, marginRight: 6, verticalAlign: -5}}/>}{user.displayName}</span>
        <span className="studio-option-id">{user.id}</span>
      </label>)}
    </fieldset>}
    <StudioField hint="Comma-separated member IDs, for members not listed." label="Member IDs fallback">
      <input onChange={(event) => setMemberIds(event.target.value.split(",").map((value) => value.trim()).filter(Boolean))} placeholder="U0123456789" type="text" value={memberIds.join(", ")}/>
    </StudioField>
    <SaveAction disabled={busy || (target.required && memberIds.length === 0)} onSave={() => onSave({memberIds})}/>
  </UnitFrame>;
}

function TextInputUnit({target, busy, onSave}: SlackUnitProps) {
  const [value, setValue] = useState(stringValue(target.value.text));
  return <UnitFrame target={target}>
    <StudioField label={target.label}><input onChange={(event) => setValue(event.target.value)} type="text" value={value}/></StudioField>
    <SaveAction disabled={busy || (target.required && value.length === 0)} onSave={() => onSave({text: value})}/>
  </UnitFrame>;
}

function UnitFrame({target, loadError, children}: {target: ConnectorStudioConfigurationUnitTarget; loadError?: string; children: ReactNode}) {
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    {children}
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

function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
function stringArray(value: unknown): string[] { return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : []; }
