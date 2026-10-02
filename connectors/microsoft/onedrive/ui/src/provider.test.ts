// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { canBrowseItem, driveKeyFromDriveID, parseDrive, parseDrivePage, parseFolderPage, parseSitePage, siteIDParts, skipTokenFromNextLink } from "./provider.js";

const siteID = "contoso.sharepoint.com,da60e844-ba1d-49bc-b4d4-d5e36bae9019,712a596e-90a1-49e3-9b48-bfa80bee8740";

describe("Microsoft Graph provider responses", () => {
  it("keeps sites with a usable Graph site ID and reads the next page token", () => {
    expect(parseSitePage({
      value: [{id: siteID, displayName: "Finance", webUrl: "https://contoso.sharepoint.com/sites/finance"}, {id: "not-a-site", displayName: "Bad"}],
      "@odata.nextLink": "https://graph.microsoft.com/v1.0/sites?search=fin&$skiptoken=abc123",
    })).toEqual({items: [{id: siteID, name: "Finance", webUrl: "https://contoso.sharepoint.com/sites/finance"}], nextSkipToken: "abc123"});
  });

  it("splits a site ID into the comma-free path parameters Dex Web accepts", () => {
    expect(siteIDParts(siteID)).toEqual({
      siteHostname: "contoso.sharepoint.com", siteCollectionId: "da60e844-ba1d-49bc-b4d4-d5e36bae9019", siteWebId: "712a596e-90a1-49e3-9b48-bfa80bee8740",
    });
    for (const invalid of ["", "contoso.sharepoint.com", "evil/../x,da60e844-ba1d-49bc-b4d4-d5e36bae9019,712a596e-90a1-49e3-9b48-bfa80bee8740", `${siteID},extra`]) {
      expect(siteIDParts(invalid)).toBeUndefined();
    }
  });

  it("reads drives and the b! key that the folder commands send", () => {
    expect(parseDrivePage({value: [{id: "b!-RIj2DuyvEyV1T4N_lOaMHk8", name: "Documents", driveType: "documentLibrary"}, {id: "bad id"}]}).items)
      .toEqual([{id: "b!-RIj2DuyvEyV1T4N_lOaMHk8", name: "Documents", driveType: "documentLibrary"}]);
    expect(parseDrive({id: "b!me_1", name: "OneDrive", driveType: "business"})).toEqual([{id: "b!me_1", name: "OneDrive", driveType: "business"}]);
    expect(driveKeyFromDriveID("b!-RIj2DuyvEyV1T4N_lOaMHk8")).toBe("-RIj2DuyvEyV1T4N_lOaMHk8");
    expect(driveKeyFromDriveID("D4648F06C91D9D3D")).toBe("");
    expect(driveKeyFromDriveID("b!has!bang")).toBe("");
  });

  it("keeps only folders and rejects next links outside Microsoft Graph", () => {
    expect(parseFolderPage({value: [{id: "01FOLDER", name: "Reports", folder: {childCount: 2}}, {id: "01FILE", name: "a.txt", file: {}}]}).items)
      .toEqual([{id: "01FOLDER", name: "Reports"}]);
    expect(skipTokenFromNextLink("https://evil.example/v1.0/x?$skiptoken=1")).toBe("");
    expect(skipTokenFromNextLink("http://graph.microsoft.com/v1.0/x?$skiptoken=1")).toBe("");
    expect(skipTokenFromNextLink("not a url")).toBe("");
    expect(canBrowseItem("01BYE5RZ6QN3ZWBTUFOFD3GSPGOHDJD36K")).toBe(true);
    expect(canBrowseItem("D4648F06C91D9D3D!54927")).toBe(false);
  });
});
