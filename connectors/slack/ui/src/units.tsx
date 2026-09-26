import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { useState, type ReactNode } from "react";

export interface SlackChannel { id: string; name: string; isPrivate?: boolean; }
export interface SlackUser { id: string; displayName: string; imageUrl?: string; }

export interface SlackUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  channels: SlackChannel[];
  users: SlackUser[];
  busy?: boolean;
  onLoadChannels(): void;
  onLoadUsers(): void;
  onSave(value: Record<string, unknown>): void;
}

export function SlackConfigurationUnit(props: SlackUnitProps) {
  switch (props.target.unitId) {
    case "channelPicker": return <ChannelPickerUnit {...props}/>;
    case "memberPicker": return <MemberPickerUnit {...props}/>;
    case "textInput": return <TextInputUnit {...props}/>;
    default: return <p role="alert">Unsupported Slack configuration unit: {props.target.unitId}</p>;
  }
}

function ChannelPickerUnit({target, channels, busy, onLoadChannels, onSave}: SlackUnitProps) {
  const [channelId, setChannelId] = useState(stringValue(target.value.channelId));
  return <UnitFrame target={target}>
    <button disabled={busy} onClick={onLoadChannels} type="button">Load joined channels</button>
    <p className="note">Only channels this app has joined are available.</p>
    <label>Channel<select value={channelId} onChange={(event) => setChannelId(event.target.value)}>
      <option value="">Select a channel</option>
      {channels.map((channel) => <option key={channel.id} value={channel.id}>#{channel.name}{channel.isPrivate ? " (private)" : ""} · {channel.id}</option>)}
    </select></label>
    <label>Channel ID fallback<input value={channelId} placeholder="C0123456789" onChange={(event) => setChannelId(event.target.value)}/></label>
    <button disabled={busy || (target.required && channelId.length === 0)} onClick={() => onSave({channelId})} type="button">Save</button>
  </UnitFrame>;
}

function MemberPickerUnit({target, users, busy, onLoadUsers, onSave}: SlackUnitProps) {
  const [memberIds, setMemberIds] = useState(stringArray(target.value.memberIds));
  return <UnitFrame target={target}>
    <button disabled={busy} onClick={onLoadUsers} type="button">Load members</button>
    {users.map((user) => <label className="user" key={user.id}>
      <input type="checkbox" checked={memberIds.includes(user.id)} onChange={(event) => setMemberIds(event.target.checked ? [...memberIds, user.id] : memberIds.filter((id) => id !== user.id))}/>
      {user.imageUrl && <img src={user.imageUrl} alt=""/>}<span>{user.displayName}<small>{user.id}</small></span>
    </label>)}
    <label>Member IDs fallback<input value={memberIds.join(", ")} placeholder="U0123456789" onChange={(event) => setMemberIds(event.target.value.split(",").map((value) => value.trim()).filter(Boolean))}/></label>
    <button disabled={busy || (target.required && memberIds.length === 0)} onClick={() => onSave({memberIds})} type="button">Save</button>
  </UnitFrame>;
}

function TextInputUnit({target, onSave}: SlackUnitProps) {
  const [value, setValue] = useState(stringValue(target.value.text));
  return <UnitFrame target={target}><label>{target.label}<input value={value} onChange={(event) => setValue(event.target.value)}/></label><button disabled={target.required && value.length === 0} onClick={() => onSave({text: value})} type="button">Save</button></UnitFrame>;
}

function UnitFrame({target, children}: {target: ConnectorStudioConfigurationUnitTarget; children: ReactNode}) {
  return <section className="unit"><h2>{target.label}</h2>{target.description && <p>{target.description}</p>}{children}</section>;
}

function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
function stringArray(value: unknown): string[] { return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : []; }
