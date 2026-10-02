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
import { channelIDPattern, chatIDPattern, teamIDPattern, type TeamsChannel, type TeamsChat, type TeamsTeam } from "./provider.js";

export const teamIDHint = "Use a team ID when the team is not listed: in Teams choose the team's More options > Get link to team; the groupId value in the link is the team ID, a GUID such as fbe2bf47-16c8-47cf-b4a5-4b9b187c508b.";
export const channelIDHint = "Use a channel ID when the channel is not listed: in Teams choose the channel's More options > Get link to channel; the part after /channel/, URL-decoded, is the channel ID, such as 19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2.";
export const chatIDHint = "Use a chat ID when the chat is not listed: open the chat in Teams on the web at https://teams.microsoft.com; the part of the address that starts with 19: and ends before the next / is the chat ID, such as 19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2.";

export interface TeamsUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  teams?: TeamsTeam[];
  channels?: TeamsChannel[];
  chats?: TeamsChat[];
  /** isListTruncated reports that paging stopped before Graph's last page. */
  isListTruncated?: boolean;
  busy?: boolean;
  loadError?: string;
  onLoadTeams(): void;
  onLoadChannels(teamId: string): void;
  onLoadChats(): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function TeamsConfigurationUnit(props: TeamsUnitProps) {
  if (props.target.unitId === "teamPicker") return <TeamPickerUnit {...props}/>;
  if (props.target.unitId === "channelPicker") return <ChannelPickerUnit {...props}/>;
  if (props.target.unitId === "chatPicker") return <ChatPickerUnit {...props}/>;
  return <StudioNotice tone="error">Unsupported Microsoft Teams configuration unit: {props.target.unitId}</StudioNotice>;
}

function TeamPickerUnit({target, teams = [], busy, loadError, onLoadTeams, onSave}: TeamsUnitProps) {
  const [teamId, setTeamId] = useState(stringValue(target.value.teamId));
  const [teamName, setTeamName] = useState(stringValue(target.value.teamName));
  const isValid = teamIDPattern.test(teamId);
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadTeams}>Load teams</StudioButton>
      {teamName && <span className="studio-muted"><strong>Team:</strong> {teamName}</span>}
    </div>
    {teams.length > 0 && <StudioField label="Team">
      <select onChange={(event) => { setTeamId(event.target.value); setTeamName(teams.find((team) => team.id === event.target.value)?.name ?? ""); }} value={teamId}>
        <option value="">Select a team</option>
        {teams.map((team) => <option key={team.id} value={team.id}>{team.name}</option>)}
      </select>
    </StudioField>}
    <StudioField hint={teamIDHint} label="Team ID">
      <input onChange={(event) => { setTeamId(event.target.value.trim()); setTeamName(""); }} placeholder="fbe2bf47-16c8-47cf-b4a5-4b9b187c508b" type="text" value={teamId}/>
    </StudioField>
    {teamId !== "" && !isValid && <StudioNotice tone="error">A team ID is a GUID such as fbe2bf47-16c8-47cf-b4a5-4b9b187c508b.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave({teamId: teamId.toLowerCase(), teamName})}/>
  </UnitFrame>;
}

function ChannelPickerUnit({target, channels = [], isListTruncated, busy, loadError, onLoadChannels, onSave}: TeamsUnitProps) {
  const [channelId, setChannelId] = useState(stringValue(target.value.channelId));
  const [channelName, setChannelName] = useState(stringValue(target.value.channelName));
  const teamId = stringValue(target.value.teamId);
  const isValid = channelIDPattern.test(channelId);
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy || !teamIDPattern.test(teamId)} onClick={() => onLoadChannels(teamId)}>Load channels</StudioButton>
      {!teamIDPattern.test(teamId) && <span className="studio-muted">Save a team first to list its channels.</span>}
      {channelName && <span className="studio-muted"><strong>Channel:</strong> {channelName}</span>}
    </div>
    {channels.length > 0 && <StudioField label="Channel">
      <select onChange={(event) => { setChannelId(event.target.value); setChannelName(channels.find((channel) => channel.id === event.target.value)?.name ?? ""); }} value={channelId}>
        <option value="">Select a channel</option>
        {channels.map((channel) => <option key={channel.id} value={channel.id}>{describeChannel(channel)}</option>)}
      </select>
    </StudioField>}
    {isListTruncated && <StudioNotice tone="attention">The team has more channels than one list can show. Enter a channel ID to use one that is not listed.</StudioNotice>}
    <StudioField hint={channelIDHint} label="Channel ID">
      <input onChange={(event) => { setChannelId(event.target.value.trim()); setChannelName(""); }} placeholder="19:4a95f7d8db4c4e7fae857bcebe0623e6@thread.tacv2" type="text" value={channelId}/>
    </StudioField>
    {channelId !== "" && !isValid && <StudioNotice tone="error">A channel ID starts with 19: and ends with @thread. followed by letters and digits, such as @thread.tacv2.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave({channelId, channelName})}/>
  </UnitFrame>;
}

function ChatPickerUnit({target, chats = [], isListTruncated, busy, loadError, onLoadChats, onSave}: TeamsUnitProps) {
  const [chatId, setChatId] = useState(stringValue(target.value.chatId));
  const [chatName, setChatName] = useState(stringValue(target.value.chatName));
  const isValid = chatId === "" ? !target.required : chatIDPattern.test(chatId);
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadChats}>Load chats</StudioButton>
      {chatName && <span className="studio-muted"><strong>Chat:</strong> {chatName}</span>}
    </div>
    {chats.length > 0 && <StudioField label="Chat">
      <select onChange={(event) => { setChatId(event.target.value); setChatName(chats.find((chat) => chat.id === event.target.value)?.label ?? ""); }} value={chatId}>
        <option value="">Select a chat</option>
        {chats.map((chat) => <option key={chat.id} value={chat.id}>{chat.label} ({chat.chatType})</option>)}
      </select>
    </StudioField>}
    {isListTruncated && <StudioNotice tone="attention">The account has more chats than one list can show. Enter a chat ID to use one that is not listed.</StudioNotice>}
    <StudioField hint={chatIDHint} label="Chat ID">
      <input onChange={(event) => { setChatId(event.target.value.trim()); setChatName(""); }} placeholder="19:meeting_MjdhNjM4YzUtYzExZi00@thread.v2" type="text" value={chatId}/>
    </StudioField>
    {chatId !== "" && !chatIDPattern.test(chatId) && <StudioNotice tone="error">A chat ID starts with 19: and has an @ part, such as @thread.v2 or @unq.gbl.spaces.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave({chatId, chatName})}/>
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

function describeChannel(channel: TeamsChannel): string {
  return channel.membershipType === "standard" ? channel.name : `${channel.name} (${channel.membershipType})`;
}

function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
