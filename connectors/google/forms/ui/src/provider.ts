// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

export interface GoogleForm { id: string; title: string; }

const formIDPattern = /^[A-Za-z0-9_-]{1,256}$/;
// The responder link's /forms/d/e/<ID> is a published ID that the Forms API does not accept.
const responderLinkPattern = /\/forms\/(?:u\/\d+\/)?d\/e\//;
const editLinkPattern = /\/forms\/(?:u\/\d+\/)?d\/([A-Za-z0-9_-]{1,256})(?:[/?#]|$)/;

/** parseFormPage keeps the Drive form files of one listForms page that have a usable ID and title. */
export function parseFormPage(value: Record<string, unknown>): {forms: GoogleForm[]; nextPageToken: string} {
  const forms = array(value.files).flatMap((item) => record(item) && text(item.id) && formIDPattern.test(item.id) && text(item.name) ? [{id: item.id, title: item.name}] : []);
  return {forms, nextPageToken: text(value.nextPageToken) ? value.nextPageToken : ""};
}

/** formIDFromInput accepts a form ID or a docs.google.com/forms/d/<ID>/edit link and returns the ID, or "" when neither matches. */
export function formIDFromInput(value: string): string {
  const trimmed = value.trim();
  if (formIDPattern.test(trimmed)) return trimmed;
  if (isResponderLink(trimmed)) return "";
  return editLinkPattern.exec(trimmed)?.[1] ?? "";
}

/** isResponderLink reports a form's /viewform link, whose ID is not the form ID. */
export function isResponderLink(value: string): boolean {
  return responderLinkPattern.test(value.trim());
}

function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
