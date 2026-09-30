// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export interface DriveFolder { id: string; name: string; }

const driveIDPattern = /^[A-Za-z0-9_-]{1,256}$/;
const folderURLPattern = /\/folders\/([A-Za-z0-9_-]{1,256})(?:[/?#]|$)/;

export function parseFolderPage(value: Record<string, unknown>): {folders: DriveFolder[]; nextPageToken: string} {
  const folders = array(value.files).flatMap((item) => record(item) && text(item.id) && driveIDPattern.test(item.id) && text(item.name) ? [{id: item.id, name: item.name}] : []);
  return {folders, nextPageToken: text(value.nextPageToken) ? value.nextPageToken : ""};
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
