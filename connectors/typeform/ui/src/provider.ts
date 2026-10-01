// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

/** formListCapability is the backend capability both form list commands declare. */
export const formListCapability = "typeform.forms-list";

/** formListCommandIds lists api.typeform.com first, then the new EU data center's api.typeform.eu. */
export const formListCommandIds = ["listForms", "listFormsNewEu"];

const maximumFormPages = 20;
const formIDPattern = /^[A-Za-z0-9_-]{1,128}$/;

export interface TypeformForm { id: string; title: string; isPublic: boolean; }
export interface TypeformFormPage { forms: TypeformForm[]; nextPage: number; }
export interface TypeformFormList { forms: TypeformForm[]; isTruncated: boolean; }

/** ExecuteFormListCommand runs one declared form list command with its query parameters. */
export type ExecuteFormListCommand = (commandId: string, parameters: Record<string, string>) => Promise<Record<string, unknown>>;

/** parseTypeformFormPage keeps forms with an ID and returns the next page number, or 0 on the last page. */
export function parseTypeformFormPage(value: Record<string, unknown>, page: number): TypeformFormPage {
  assertTypeformOK(value);
  const forms = array(value.items).flatMap((item) => {
    if (!record(item) || !text(item.id) || !formIDPattern.test(item.id)) return [];
    const settings = record(item.settings) ? item.settings : {};
    return [{id: item.id, title: text(item.title) ? item.title : item.id, isPublic: settings.is_public !== false}];
  });
  const pageCount = typeof value.page_count === "number" && Number.isInteger(value.page_count) ? value.page_count : 0;
  return {forms, nextPage: page < pageCount ? page + 1 : 0};
}

/**
 * listTypeformForms reads every page, at most 20 of 200 forms, from the first command whose host accepts
 * the token, so a new EU data center token falls back to api.typeform.eu.
 */
export async function listTypeformForms(executeCommand: ExecuteFormListCommand): Promise<TypeformFormList> {
  let commandId = "";
  let firstPage: TypeformFormPage | undefined;
  let lastError: unknown = new Error("Typeform forms could not be loaded");
  for (const candidate of formListCommandIds) {
    try {
      firstPage = parseTypeformFormPage(await executeCommand(candidate, {page: "1"}), 1);
      commandId = candidate;
      break;
    } catch (error) {
      lastError = error;
    }
  }
  if (!firstPage) throw lastError;
  const forms = [...firstPage.forms];
  let nextPage = firstPage.nextPage;
  for (let pageCount = 1; nextPage > 0; pageCount++) {
    if (pageCount >= maximumFormPages) return {forms, isTruncated: true};
    const page = parseTypeformFormPage(await executeCommand(commandId, {page: String(nextPage)}), nextPage);
    forms.push(...page.forms);
    nextPage = page.nextPage;
  }
  return {forms, isTruncated: false};
}

/** isTypeformFormID reports whether a manually entered value is a form ID, the part after /to/ in a form link. */
export function isTypeformFormID(value: string): boolean {
  return formIDPattern.test(value);
}

// assertTypeformOK reports an error object in connector words; Typeform's own text never reaches the frame.
function assertTypeformOK(value: Record<string, unknown>) {
  if (text(value.code) && !("items" in value)) throw new Error("Typeform returned an error instead of the expected forms");
}

function array(value: unknown): unknown[] { return Array.isArray(value) ? value : []; }
function record(value: unknown): value is Record<string, unknown> { return typeof value === "object" && value !== null && !Array.isArray(value); }
function text(value: unknown): value is string { return typeof value === "string" && value.length > 0; }
