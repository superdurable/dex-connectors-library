import type { SlackChannel, SlackUser } from "./units.js";

export interface SlackChannelPage { channels: SlackChannel[]; nextCursor: string; }
export interface SlackUserPage { users: SlackUser[]; nextCursor: string; }

export function parseSlackChannelPage(value: Record<string, unknown>): SlackChannelPage {
  assertSlackOK(value);
  const channels = array(value.channels).flatMap((item) => {
    if (!record(item) || !text(item.id) || !text(item.name) || item.is_archived === true || item.is_member !== true) return [];
    return [{id: item.id, name: item.name, isPrivate: item.is_private === true}];
  });
  return {channels, nextCursor: nextCursor(value)};
}

export function parseSlackUserPage(value: Record<string, unknown>): SlackUserPage {
  assertSlackOK(value);
  const users = array(value.members).flatMap((item) => {
    if (!record(item) || !text(item.id) || item.deleted === true || item.is_bot === true || item.id === "USLACKBOT") return [];
    const profile = record(item.profile) ? item.profile : {};
    const displayName = firstText(profile.display_name, profile.real_name, item.name, item.id);
    return [{id: item.id, displayName, imageUrl: text(profile.image_48) ? profile.image_48 : undefined}];
  });
  return {users, nextCursor: nextCursor(value)};
}

function assertSlackOK(value: Record<string, unknown>) {
  if (value.ok !== true) throw new Error(text(value.error) || "Slack rejected the request");
}

function nextCursor(value: Record<string, unknown>): string {
  const metadata = record(value.response_metadata) ? value.response_metadata : {};
  return text(metadata.next_cursor) ? metadata.next_cursor.trim() : "";
}

function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
function firstText(...values: unknown[]): string { return values.find(text) as string; }
