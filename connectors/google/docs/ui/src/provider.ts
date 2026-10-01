// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export interface DocsDocument { id: string; title: string; modifiedTime: string; }
export interface DriveFolder { id: string; name: string; }

const driveIDPattern = /^[A-Za-z0-9_-]{1,256}$/;
const documentURLPattern = /\/document\/(?:u\/\d+\/)?d\/([A-Za-z0-9_-]{1,256})(?:[/?#]|$)/;
const folderURLPattern = /\/folders\/([A-Za-z0-9_-]{1,256})(?:[/?#]|$)/;

/** parseDocumentPage keeps the listed Google Docs that have a usable ID and title. */
export function parseDocumentPage(value: Record<string, unknown>): {documents: DocsDocument[]; nextPageToken: string} {
  const documents = array(value.files).flatMap((item) => record(item) && text(item.id) && driveIDPattern.test(item.id) && text(item.name)
    ? [{id: item.id, title: item.name, modifiedTime: text(item.modifiedTime) ? item.modifiedTime : ""}]
    : []);
  return {documents, nextPageToken: text(value.nextPageToken) ? value.nextPageToken : ""};
}

/** parseFolderPage keeps the listed Drive folders that have a usable ID and name. */
export function parseFolderPage(value: Record<string, unknown>): {folders: DriveFolder[]; nextPageToken: string} {
  const folders = array(value.files).flatMap((item) => record(item) && text(item.id) && driveIDPattern.test(item.id) && text(item.name) ? [{id: item.id, name: item.name}] : []);
  return {folders, nextPageToken: text(value.nextPageToken) ? value.nextPageToken : ""};
}

/** documentIDFromInput accepts a document ID or a docs.google.com/document link and returns the ID, or "" when neither matches. */
export function documentIDFromInput(value: string): string {
  const trimmed = value.trim();
  if (driveIDPattern.test(trimmed)) return trimmed;
  return documentURLPattern.exec(trimmed)?.[1] ?? "";
}

/** folderIDFromInput accepts a folder ID or a drive.google.com folder URL and returns the ID, or "" when neither matches. */
export function folderIDFromInput(value: string): string {
  const trimmed = value.trim();
  if (driveIDPattern.test(trimmed)) return trimmed;
  return folderURLPattern.exec(trimmed)?.[1] ?? "";
}

function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
