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

export interface DriveMimeTypeOption { mimeType: string; label: string; }

/** driveMimeTypeOptions lists Google Workspace types from Drive's MIME type guide and common uploads. */
export const driveMimeTypeOptions: readonly DriveMimeTypeOption[] = [
  {mimeType: "application/vnd.google-apps.document", label: "Google Docs"},
  {mimeType: "application/vnd.google-apps.spreadsheet", label: "Google Sheets"},
  {mimeType: "application/vnd.google-apps.presentation", label: "Google Slides"},
  {mimeType: "application/vnd.google-apps.form", label: "Google Forms"},
  {mimeType: "application/vnd.google-apps.folder", label: "Google Drive folder"},
  {mimeType: "application/pdf", label: "PDF"},
  {mimeType: "text/plain", label: "Plain text"},
  {mimeType: "text/csv", label: "CSV"},
  {mimeType: "image/jpeg", label: "JPEG image"},
  {mimeType: "image/png", label: "PNG image"},
  {mimeType: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", label: "Microsoft Word (.docx)"},
  {mimeType: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", label: "Microsoft Excel (.xlsx)"},
];

const plainMediaTypePattern = /^[a-z0-9][a-z0-9!#$&^_.+-]*\/[a-z0-9][a-z0-9!#$&^_.+-]*$/;

/** mimeTypeFromInput returns the trimmed value when it is one lowercase type/subtype without parameters, or "" otherwise. */
export function mimeTypeFromInput(value: string): string {
  const trimmed = value.trim();
  return plainMediaTypePattern.test(trimmed) ? trimmed : "";
}

function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
