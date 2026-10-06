// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { collectProviderPages, type ConnectorStudioClient } from "@superdurable/dex-connectors-react";

/** FrontResource is one inbox, teammate, or tag projected for a picker; id is what the unit stores. */
export interface FrontResource {
  id: string;
  label: string;
  detail: string;
}

/** FrontResourcePicker describes one release-owned picker unit and the manifest command that lists its resources. */
export interface FrontResourcePicker {
  unitId: "inboxPicker" | "teammatePicker" | "tagPicker";
  port: "inboxId" | "teammateId" | "tagId";
  noun: string;
  pluralNoun: string;
  indefiniteNoun: string;
  commandId: string;
  capability: string;
  idPattern: RegExp;
  idExample: string;
  isPaged: boolean;
  fallbackHint: string;
}

/** frontResourcePickers maps each unit ID to its picker; the commands and capabilities match connector.yaml. */
export const frontResourcePickers: Readonly<Record<FrontResourcePicker["unitId"], FrontResourcePicker>> = Object.freeze({
  inboxPicker: {
    unitId: "inboxPicker", port: "inboxId", noun: "inbox", pluralNoun: "inboxes", indefiniteNoun: "an inbox",
    commandId: "listInboxes", capability: "front.inboxes-list",
    idPattern: /^inb_[A-Za-z0-9]{1,40}$/, idExample: "inb_41w25", isPaged: false,
    fallbackHint: "When the list cannot load, call GET https://api2.frontapp.com/inboxes with this connection's API token as a bearer token and copy the inbox's id field; it starts with inb_.",
  },
  teammatePicker: {
    unitId: "teammatePicker", port: "teammateId", noun: "teammate", pluralNoun: "teammates", indefiniteNoun: "a teammate",
    commandId: "listTeammates", capability: "front.teammates-list",
    idPattern: /^tea_[A-Za-z0-9]{1,40}$/, idExample: "tea_2thf", isPaged: false,
    fallbackHint: "When the list cannot load, call GET https://api2.frontapp.com/teammates with this connection's API token as a bearer token and copy the teammate's id field; it starts with tea_.",
  },
  tagPicker: {
    unitId: "tagPicker", port: "tagId", noun: "tag", pluralNoun: "tags", indefiniteNoun: "a tag",
    commandId: "listTags", capability: "front.tags-list",
    idPattern: /^tag_[A-Za-z0-9]{1,40}$/, idExample: "tag_13o8r1", isPaged: true,
    fallbackHint: "When the list cannot load, call GET https://api2.frontapp.com/tags with this connection's API token as a bearer token and copy the tag's id field; it starts with tag_, and a tag name does not work.",
  },
});

/** FrontResourcePage is one parsed list and the page token for the next page, empty on the last page. */
export interface FrontResourcePage {
  resources: FrontResource[];
  nextPageToken: string;
}

const pageTokenPattern = /^[A-Za-z0-9._~-]{1,1000}$/;
const companyAPIHostPattern = /^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.api\.frontapp\.com$/;

/**
 * loadFrontResources lists the picker's resources through the host broker, following Front's page tokens
 * for tags, and returns them sorted by label. It rejects with the host's error when a page fails, so the
 * unit offers manual ID entry.
 */
export async function loadFrontResources(
  client: Pick<ConnectorStudioClient, "executeProviderCommand">, picker: FrontResourcePicker,
): Promise<{resources: FrontResource[]; isTruncated: boolean}> {
  const {items, isTruncated} = await collectProviderPages(async (cursor) => {
    const page = parseFrontResourcePage(picker, await client.executeProviderCommand(
      picker.commandId, picker.capability, cursor === "" ? {} : {pageToken: cursor},
    ));
    return {items: page.resources, nextCursor: picker.isPaged ? page.nextPageToken : ""};
  });
  const resourcesByID = new Map<string, FrontResource>();
  for (const resource of items) {
    if (!resourcesByID.has(resource.id)) resourcesByID.set(resource.id, resource);
  }
  const resources = [...resourcesByID.values()];
  return {resources: resources.sort((left, right) => left.label.localeCompare(right.label) || left.id.localeCompare(right.id)), isTruncated};
}

/** parseFrontResourcePage keeps the resources with a valid ID from one `_results` list. */
export function parseFrontResourcePage(picker: FrontResourcePicker, value: Record<string, unknown>): FrontResourcePage {
  if (!Array.isArray(value._results)) throw new Error(`Front did not return a ${picker.noun} list`);
  const resources = value._results.flatMap((item): FrontResource[] => {
    if (!isRecord(item) || typeof item.id !== "string" || !picker.idPattern.test(item.id)) return [];
    return [projectResource(picker, item, item.id)];
  });
  const pagination = isRecord(value._pagination) ? value._pagination : {};
  return {resources, nextPageToken: readNextPageToken(pagination.next)};
}

/** isFrontResourceID reports whether value is an ID of the picker's resource, such as tag_13o8r1. */
export function isFrontResourceID(picker: FrontResourcePicker, value: string): boolean {
  return picker.idPattern.test(value);
}

function projectResource(picker: FrontResourcePicker, item: Record<string, unknown>, id: string): FrontResource {
  const name = text(item.name);
  if (picker.unitId === "teammatePicker") {
    const fullName = [text(item.first_name), text(item.last_name)].filter((part) => part !== "").join(" ");
    const email = text(item.email);
    return {id, label: fullName || text(item.username) || email || id, detail: [email, item.is_blocked === true ? "blocked" : ""].filter((part) => part !== "").join(" · ")};
  }
  return {id, label: name || id, detail: item.is_private === true ? "private" : ""};
}

/** readNextPageToken takes page_token from a next link on Front's API hosts; any other link ends the list. */
function readNextPageToken(next: unknown): string {
  if (typeof next !== "string" || next === "") return "";
  let parsed: URL;
  try {
    parsed = new URL(next);
  } catch {
    return "";
  }
  const isFrontHost = parsed.protocol === "https:" && parsed.username === "" && parsed.password === "" &&
    (parsed.host === "api2.frontapp.com" || companyAPIHostPattern.test(parsed.host));
  const token = parsed.searchParams.get("page_token") ?? "";
  return isFrontHost && parsed.pathname.startsWith("/tags") && pageTokenPattern.test(token) ? token : "";
}

function text(value: unknown): string { return typeof value === "string" ? value.trim() : ""; }
function isRecord(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
