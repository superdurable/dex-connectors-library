// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorConnectionView } from "@superdurable/dex-connectors-react";

/** MailFolder is one Graph mailFolder with the path of display names that leads to it. */
export interface MailFolder {
  id: string;
  displayName: string;
  childFolderCount: number;
  path: string;
}

/** MailFolderPage is one page of mailFolders; nextSkip is the $skip of the next page, or empty on the last page. */
export interface MailFolderPage {
  folders: MailFolder[];
  nextSkip: string;
}

/** ProviderCommandRequest names one declared Studio command and its parameters. */
export interface ProviderCommandRequest {
  commandId: string;
  parameters: Record<string, string>;
}

/** MailFolderCommands builds the folder list requests for one connection, or explains why it cannot. */
export type MailFolderCommands =
  | {isAvailable: true; topLevel(skip: string): ProviderCommandRequest; children(folderId: string, skip: string): ProviderCommandRequest}
  | {isAvailable: false; reason: string};

export const mailFoldersCapability = "outlookmail.mail-folders-list";

/** wellKnownFolderNames are Graph's well-known mail folder names, which the connector accepts in place of an ID. */
export const wellKnownFolderNames = [
  "archive", "deleteditems", "drafts", "inbox", "junkemail", "outbox", "sentitems", "clutter", "conflicts",
  "conversationhistory", "localfailures", "msgfolderroot", "recoverableitemsdeletions", "scheduled",
  "searchfolders", "serverfailures", "syncissues",
] as const;

const folderIDPattern = /^[A-Za-z0-9=+_-]{1,1024}$/;

/**
 * mailFolderCommands lists /me folders for a delegated connection and /users/{mailbox} folders for an
 * app-only connection, whose mailbox comes from the connection's saved configuration.
 */
export function mailFolderCommands(connection: ConnectorConnectionView): MailFolderCommands {
  const isAppOnly = (connection.authMethodIds ?? []).includes("app-only");
  if (!isAppOnly) {
    return {
      isAvailable: true,
      topLevel: (skip) => ({commandId: "listMailFolders", parameters: skipParameter(skip)}),
      children: (folderId, skip) => ({commandId: "listChildMailFolders", parameters: {folderId, ...skipParameter(skip)}}),
    };
  }
  const mailbox = connection.configuration?.mailbox;
  if (typeof mailbox !== "string" || mailbox.trim() === "") {
    return {isAvailable: false, reason: "Save the connection's mailbox field first; app-only folders are listed for that mailbox."};
  }
  return {
    isAvailable: true,
    topLevel: (skip) => ({commandId: "listMailboxMailFolders", parameters: {mailbox: mailbox.trim(), ...skipParameter(skip)}}),
    children: (folderId, skip) => ({commandId: "listMailboxChildMailFolders", parameters: {mailbox: mailbox.trim(), folderId, ...skipParameter(skip)}}),
  };
}

/**
 * parseMailFolderPage keeps folders with a usable ID and a name from one Graph mailFolders page. A Graph error
 * object is reported in connector words, never with Graph's message text.
 */
export function parseMailFolderPage(value: Record<string, unknown>, parentPath: string, requestedSkip: number): MailFolderPage {
  if (!Array.isArray(value.value)) throw new Error("Microsoft Graph returned an error instead of the folder list");
  const folders = value.value.flatMap((item): MailFolder[] => {
    if (!record(item) || typeof item.id !== "string" || !folderIDPattern.test(item.id) || !text(item.displayName)) return [];
    const childFolderCount = typeof item.childFolderCount === "number" && Number.isSafeInteger(item.childFolderCount) && item.childFolderCount > 0
      ? item.childFolderCount : 0;
    return [{id: item.id, displayName: item.displayName, childFolderCount, path: parentPath ? `${parentPath} / ${item.displayName}` : item.displayName}];
  });
  const hasNextPage = typeof value["@odata.nextLink"] === "string" && value.value.length > 0;
  return {folders, nextSkip: hasNextPage ? String(requestedSkip + value.value.length) : ""};
}

/** isFolderReference accepts a Graph folder ID or a well-known folder name, as moveMessage and searchMessages do. */
export function isFolderReference(value: string): boolean {
  return folderIDPattern.test(value) || (wellKnownFolderNames as readonly string[]).includes(value.toLowerCase());
}

/** mergeFolders adds newly listed folders after the ones already shown, once each. */
export function mergeFolders(current: MailFolder[], added: MailFolder[]): MailFolder[] {
  const seen = new Set(current.map((folder) => folder.id));
  return [...current, ...added.filter((folder) => !seen.has(folder.id))];
}

function skipParameter(skip: string): Record<string, string> { return skip === "" ? {} : {skip}; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
