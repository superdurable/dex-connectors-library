// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import {
  canListWorkbookContents,
  driveKeyFromDriveId,
  encodeSharingUrl,
  parseSharedWorkbook,
  parseTables,
  parseWorkbookSearchPage,
  parseWorksheets,
} from "./provider.js";

describe("Microsoft Graph workbook search", () => {
  it("keeps Excel workbooks, resolves shared items to their own drive, and reads the skip token", () => {
    expect(parseWorkbookSearchPage({
      value: [
        {id: "01OWNITEM", name: "Approvals.xlsx", file: {}, parentReference: {driveId: "b!ownDrive"}},
        {id: "01LOCALREF", name: "Shared Budget.XLSM", remoteItem: {id: "01REMOTEITEM", parentReference: {driveId: "b!siteDrive_1"}}},
        {id: "01DOC", name: "Notes.docx", parentReference: {driveId: "b!ownDrive"}},
        {id: "01OLD", name: "Legacy.xls", parentReference: {driveId: "b!ownDrive"}},
        {id: "bad id", name: "Broken.xlsx", parentReference: {driveId: "b!ownDrive"}},
      ],
      "@odata.nextLink": "https://graph.microsoft.com/v1.0/me/drive/search(q='.xlsx')?$top=100&$skiptoken=page-2",
    })).toEqual({
      workbooks: [
        {driveId: "b!ownDrive", workbookId: "01OWNITEM", name: "Approvals.xlsx"},
        {driveId: "b!siteDrive_1", workbookId: "01REMOTEITEM", name: "Shared Budget.XLSM"},
      ],
      nextSkipToken: "page-2",
    });
  });

  it("never follows a next link off Microsoft Graph", () => {
    for (const nextLink of ["https://evil.example/v1.0/me?$skiptoken=x", "http://graph.microsoft.com/v1.0/me?$skiptoken=x", "not a url"]) {
      expect(parseWorkbookSearchPage({value: [], "@odata.nextLink": nextLink}).nextSkipToken).toBe("");
    }
    expect(() => parseWorkbookSearchPage({error: {code: "accessDenied"}})).toThrow("Microsoft Graph returned no file list");
  });
});

describe("sharing links", () => {
  it("encodes a link as Graph's unpadded base64url sharing token", () => {
    const link = "https://contoso.sharepoint.com/:x:/s/Finance/EaBcD?e=1";
    const expected = btoa(link).replace(/=+$/, "").replace(/\+/g, "-").replace(/\//g, "_");
    expect(encodeSharingUrl(`  ${link} `)).toBe(expected);
    expect(encodeSharingUrl(link)).toMatch(/^[A-Za-z0-9_-]+$/);
    expect(() => encodeSharingUrl("http://contoso.sharepoint.com/x")).toThrow("https://");
    expect(() => encodeSharingUrl("contoso")).toThrow("https://");
  });

  it("accepts only an Excel workbook behind the link", () => {
    expect(parseSharedWorkbook({id: "01SHARED", name: "Q4.xlsx", parentReference: {driveId: "b!site"}}))
      .toEqual({driveId: "b!site", workbookId: "01SHARED", name: "Q4.xlsx"});
    expect(() => parseSharedWorkbook({id: "01FOLDER", name: "Reports", folder: {}})).toThrow(".xlsx or .xlsm");
  });
});

describe("workbook contents", () => {
  it("derives the Studio path key from business drive IDs only", () => {
    expect(driveKeyFromDriveId("b!AbC-1_x")).toBe("AbC-1_x");
    expect(driveKeyFromDriveId("1a2b3c4d")).toBe("");
    expect(driveKeyFromDriveId("b!has!bang")).toBe("");
    expect(canListWorkbookContents("b!AbC", "01ITEM")).toBe(true);
    expect(canListWorkbookContents("b!AbC", "A!1")).toBe(false);
  });

  it("orders worksheets by position and keeps table IDs and names", () => {
    expect(parseWorksheets({value: [
      {id: "{B}", name: "Summary", position: 1, visibility: "Visible"},
      {id: "{A}", name: "Policy", position: 0},
      {name: "No ID", position: 2},
    ]})).toEqual([{id: "{A}", name: "Policy"}, {id: "{B}", name: "Summary"}]);
    expect(parseTables({value: [{id: "{T1}", name: "ApprovalPolicy", showHeaders: true}, {id: "2"}]}))
      .toEqual([{id: "{T1}", name: "ApprovalPolicy"}]);
    expect(() => parseTables({})).toThrow("no table list");
  });
});
