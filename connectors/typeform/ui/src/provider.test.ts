// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { isTypeformFormID, listTypeformForms, parseTypeformFormPage } from "./provider.js";

describe("Typeform provider responses", () => {
  it("keeps forms with an ID and computes the next page from page_count", () => {
    expect(parseTypeformFormPage({total_items: 3, page_count: 2, items: [
      {id: "u6nXL7", title: "Lead intake", settings: {is_public: true}},
      {id: "lT4Z3j", title: "Closed survey", settings: {is_public: false}},
      {id: "../evil", title: "Not a form ID"},
      {title: "No ID"},
    ]}, 1)).toEqual({
      forms: [
        {id: "u6nXL7", title: "Lead intake", isPublic: true},
        {id: "lT4Z3j", title: "Closed survey", isPublic: false},
      ],
      nextPage: 2,
    });
    expect(parseTypeformFormPage({total_items: 3, page_count: 2, items: [{id: "abc123"}]}, 2))
      .toEqual({forms: [{id: "abc123", title: "abc123", isPublic: true}], nextPage: 0});
  });

  it("reports an error object in connector words, never Typeform's text", () => {
    const error = {code: "AUTHENTICATION_ERROR", description: "secret detail"};
    expect(() => parseTypeformFormPage(error, 1)).toThrow("Typeform returned an error instead of the expected forms");
    expect(() => parseTypeformFormPage(error, 1)).not.toThrow("secret detail");
  });

  it("falls back to the new EU host and keeps paging on the host that accepted the token", async () => {
    const calls: string[] = [];
    const list = await listTypeformForms(async (commandId, parameters) => {
      calls.push(`${commandId}:${parameters.page}`);
      if (commandId === "listForms") throw new Error("CONNECTOR_PROVIDER_COMMAND_FAILED");
      return {page_count: 2, items: [{id: `form${parameters.page}`, title: `Form ${parameters.page}`}]};
    });
    expect(calls).toEqual(["listForms:1", "listFormsNewEu:1", "listFormsNewEu:2"]);
    expect(list).toEqual({forms: [
      {id: "form1", title: "Form 1", isPublic: true},
      {id: "form2", title: "Form 2", isPublic: true},
    ], isTruncated: false});
  });

  it("stops after 20 pages and rethrows the last error when no host accepts the token", async () => {
    const truncated = await listTypeformForms(async (_commandId, parameters) => ({page_count: 50, items: [{id: `form${parameters.page}`}]}));
    expect(truncated.forms).toHaveLength(20);
    expect(truncated.isTruncated).toBe(true);
    await expect(listTypeformForms(async (commandId) => { throw new Error(`${commandId} failed`); })).rejects.toThrow("listFormsNewEu failed");
  });

  it("accepts only form IDs as a manual fallback", () => {
    expect(isTypeformFormID("u6nXL7")).toBe(true);
    expect(isTypeformFormID("https://form.typeform.com/to/u6nXL7")).toBe(false);
    expect(isTypeformFormID("")).toBe(false);
  });
});
