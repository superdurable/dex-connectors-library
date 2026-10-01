// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { documentIDFromInput, folderIDFromInput, parseDocumentPage, parseFolderPage } from "./provider.js";

describe("Google Docs provider responses", () => {
  it("keeps document identity, title, and pagination in connector-owned code", () => {
    expect(parseDocumentPage({files: [{id: "doc-1", name: "Refund Policy", modifiedTime: "2026-09-30T12:00:00Z"}], nextPageToken: "next"})).toEqual({
      documents: [{id: "doc-1", title: "Refund Policy", modifiedTime: "2026-09-30T12:00:00Z"}], nextPageToken: "next",
    });
  });

  it("drops documents and folders without a usable ID or name", () => {
    expect(parseDocumentPage({files: [{id: "bad id", name: "Spaces"}, {id: "doc-2"}, "not a record", {id: "doc-3", name: "Ops"}]})).toEqual({
      documents: [{id: "doc-3", title: "Ops", modifiedTime: ""}], nextPageToken: "",
    });
    expect(parseFolderPage({files: [{id: "folder-1", name: "Policies"}, {id: "folder 2", name: "Bad"}], nextPageToken: "more"})).toEqual({
      folders: [{id: "folder-1", name: "Policies"}], nextPageToken: "more",
    });
  });

  it.each([
    ["1AbC_d-EF", "1AbC_d-EF"],
    ["  1AbC_d-EF  ", "1AbC_d-EF"],
    ["https://docs.google.com/document/d/1AbC_d-EF/edit", "1AbC_d-EF"],
    ["https://docs.google.com/document/u/1/d/1AbC_d-EF/edit?tab=t.0", "1AbC_d-EF"],
    ["https://docs.google.com/spreadsheets/d/1AbC_d-EF/edit", ""],
    ["not a document", ""],
    ["", ""],
  ])("reads the document ID from %j", (input, expected) => {
    expect(documentIDFromInput(input)).toBe(expected);
  });

  it.each([
    ["https://drive.google.com/drive/folders/1AbC_d-EF", "1AbC_d-EF"],
    ["https://drive.google.com/drive/my-drive", ""],
  ])("reads the folder ID from %j", (input, expected) => {
    expect(folderIDFromInput(input)).toBe(expected);
  });
});
