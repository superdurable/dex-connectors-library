import { describe, expect, it } from "vitest";
import { parseSheetTabs, parseSpreadsheetPage } from "./provider.js";

describe("Google Sheets provider responses", () => {
  it("keeps Drive spreadsheet identity and pagination in connector-owned code", () => {
    expect(parseSpreadsheetPage({files: [{id: "sheet-1", name: "Customers"}], nextPageToken: "next"})).toEqual({
      files: [{id: "sheet-1", name: "Customers"}], nextPageToken: "next",
    });
  });

  it("projects sheet titles in connector-owned code", () => {
    expect(parseSheetTabs({sheets: [{properties: {title: "Active"}}, {properties: {title: "Archive"}}]})).toEqual(["Active", "Archive"]);
  });
});
