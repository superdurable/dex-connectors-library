// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/** TeamsTeam is one team the connected account is a direct member of. */
export interface TeamsTeam { id: string; name: string; }

/** TeamsChannel is one channel of a team, with Graph's membershipType such as standard, private, or shared. */
export interface TeamsChannel { id: string; name: string; membershipType: string; }

/** TeamsChat is one chat the connected account belongs to, labeled by its topic or members. */
export interface TeamsChat { id: string; label: string; chatType: string; }

/** TeamsPage is one listed page and the $skiptoken of Graph's next-page link, or "" on the last page. */
export interface TeamsPage<T> { items: T[]; nextSkipToken: string; }

// The same rules the Go connector enforces before it calls Microsoft Graph.
export const teamIDPattern = /^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$/;
export const channelIDPattern = /^19:[A-Za-z0-9._-]{1,256}@thread\.[A-Za-z0-9]{1,32}$/;
export const chatIDPattern = /^19:[A-Za-z0-9._-]{1,256}@[A-Za-z0-9.]{1,64}$/;

/** parseJoinedTeams keeps each team's GUID and name from GET /me/joinedTeams; archived teams are listed last. */
export function parseJoinedTeams(value: Record<string, unknown>): TeamsTeam[] {
  if (!Array.isArray(value.value)) throw new Error(graphErrorText(value, "Microsoft Teams returned no team list"));
  const teams = value.value.flatMap((item): (TeamsTeam & {isArchived: boolean})[] => {
    if (!record(item) || !text(item.id) || !teamIDPattern.test(item.id)) return [];
    return [{id: item.id, name: text(item.displayName) ? item.displayName : item.id, isArchived: item.isArchived === true}];
  });
  return teams.sort((left, right) => Number(left.isArchived) - Number(right.isArchived) || left.name.localeCompare(right.name))
    .map(({id, name}) => ({id, name}));
}

/** parseChannelPage keeps each channel's ID, name, and membership type from GET /teams/{team-id}/channels. */
export function parseChannelPage(value: Record<string, unknown>): TeamsPage<TeamsChannel> {
  if (!Array.isArray(value.value)) throw new Error(graphErrorText(value, "Microsoft Teams returned no channel list"));
  const items = value.value.flatMap((item): TeamsChannel[] => {
    if (!record(item) || !text(item.id) || !channelIDPattern.test(item.id)) return [];
    return [{id: item.id, name: text(item.displayName) ? item.displayName : item.id, membershipType: text(item.membershipType) ? item.membershipType : "standard"}];
  });
  return {items, nextSkipToken: skipTokenOf(value["@odata.nextLink"])};
}

/** parseChatPage labels each chat from GET /me/chats?$expand=members by its topic, or else its members' names. */
export function parseChatPage(value: Record<string, unknown>): TeamsPage<TeamsChat> {
  if (!Array.isArray(value.value)) throw new Error(graphErrorText(value, "Microsoft Teams returned no chat list"));
  const items = value.value.flatMap((item): TeamsChat[] => {
    if (!record(item) || !text(item.id) || !chatIDPattern.test(item.id)) return [];
    const chatType = text(item.chatType) ? item.chatType : "group";
    const memberNames = Array.isArray(item.members)
      ? item.members.flatMap((member) => record(member) && text(member.displayName) ? [member.displayName] : [])
      : [];
    const label = text(item.topic) ? item.topic : memberNames.length > 0 ? memberNames.join(", ") : item.id;
    return [{id: item.id, label, chatType}];
  });
  return {items, nextSkipToken: skipTokenOf(value["@odata.nextLink"])};
}

/** skipTokenOf reads $skiptoken from a next-page link on graph.microsoft.com; any other link ends paging. */
export function skipTokenOf(nextLink: unknown): string {
  if (!text(nextLink)) return "";
  try {
    const link = new URL(nextLink);
    if (link.protocol !== "https:" || link.hostname !== "graph.microsoft.com") return "";
    let skipToken = "";
    link.searchParams.forEach((value, name) => {
      if (skipToken === "" && name.toLowerCase() === "$skiptoken") skipToken = value;
    });
    return skipToken;
  } catch {
    return "";
  }
}

// graphErrorText names only Graph's machine-readable error.code, never its message text.
function graphErrorText(value: Record<string, unknown>, fallback: string): string {
  const code = record(value.error) ? value.error.code : undefined;
  return text(code) && /^[A-Za-z][A-Za-z0-9_.]{0,63}$/.test(code) ? `${fallback} (${code})` : fallback;
}

function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.trim().length > 0; }
