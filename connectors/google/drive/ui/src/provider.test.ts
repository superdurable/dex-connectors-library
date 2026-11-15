// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { driveMimeTypeOptions, folderIDFromInput, mimeTypeFromInput, parseFolderPage } from "./provider.js";

describe("Google Drive provider responses", () => {
  it("keeps Drive folder identity and pagination in connector-owned code", () => {
    expect(parseFolderPage({files: [{id: "folder-1", name: "Finance"}], nextPageToken: "next"})).toEqual({
      folders: [{id: "folder-1", name: "Finance"}], nextPageToken: "next",
    });
  });

  it("drops folders without a usable ID or name", () => {
    expect(parseFolderPage({files: [{id: "bad id", name: "Spaces"}, {id: "folder-2"}, "not a record", {id: "folder-3", name: "Ops"}]})).toEqual({
      folders: [{id: "folder-3", name: "Ops"}], nextPageToken: "",
    });
  });

  it.each([
    ["1AbC_d-EF", "1AbC_d-EF"],
    ["  1AbC_d-EF  ", "1AbC_d-EF"],
    ["https://drive.google.com/drive/folders/1AbC_d-EF", "1AbC_d-EF"],
    ["https://drive.google.com/drive/u/0/folders/1AbC_d-EF?usp=sharing", "1AbC_d-EF"],
    ["https://drive.google.com/drive/my-drive", ""],
    ["not a folder", ""],
    ["", ""],
  ])("reads the folder ID from %j", (input, expected) => {
    expect(folderIDFromInput(input)).toBe(expected);
  });

  it.each([
    ["application/pdf", "application/pdf"],
    ["  application/vnd.google-apps.document ", "application/vnd.google-apps.document"],
    ["Application/PDF", ""],
    ["text/plain; charset=utf-8", ""],
    ["pdf", ""],
    ["", ""],
  ])("reads the MIME type from %j", (input, expected) => {
    expect(mimeTypeFromInput(input)).toBe(expected);
  });

  it("lists only valid, unique MIME types", () => {
    const listed = driveMimeTypeOptions.map((option) => option.mimeType);
    expect(new Set(listed).size).toBe(listed.length);
    for (const mimeType of listed) expect(mimeTypeFromInput(mimeType)).toBe(mimeType);
  });
});
