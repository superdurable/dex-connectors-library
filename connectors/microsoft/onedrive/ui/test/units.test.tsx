// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { renderUnit, unitTarget } from "./render.js";

describe("drive picker", () => {
  it("offers the signed-in user's OneDrive when no site is saved", () => {
    const markup = renderUnit({target: unitTarget("drivePicker")});
    expect(markup).toContain(">Use my OneDrive<");
    expect(markup).toContain("Save a site first to list its libraries.");
    expect(markup).toContain("Leave blank for the signed-in user&#x27;s OneDrive; app-only connections need a drive ID.");
  });

  it("lists the saved site's libraries and keeps the picked drive", () => {
    const markup = renderUnit({
      target: unitTarget("drivePicker", {value: {siteId: "contoso.sharepoint.com,a,b", driveId: "b!lib_1", driveName: "Documents"}}),
      drives: {items: [{id: "b!lib_1", name: "Documents", driveType: "documentLibrary"}], isTruncated: false},
    });
    expect(markup).toContain(">List document libraries<");
    expect(markup).toContain('<option value="b!lib_1" selected="">Documents</option>');
    expect(markup).toContain("<strong>Drive:</strong> Documents");
  });

  it("rejects a drive ID with characters Graph never uses", () => {
    const markup = renderUnit({target: unitTarget("drivePicker", {value: {driveId: "b!a/b"}})});
    expect(markup).toContain('role="alert">Enter a Microsoft Graph drive ID');
  });
});

describe("folder picker", () => {
  it("asks for a saved drive before listing folders", () => {
    const markup = renderUnit({target: unitTarget("folderPicker")});
    expect(markup).toContain("Save a drive in this Step first");
    expect(markup).toContain('type="button" disabled="">List folders');
  });

  it("lists folders of a b! drive and explains blank as the drive root", () => {
    const markup = renderUnit({
      target: unitTarget("folderPicker", {value: {driveId: "b!lib_1", folderId: "01REPORTS", folderName: "Reports"}}),
      folders: {items: [{id: "01REPORTS", name: "Reports"}], isTruncated: false},
    });
    expect(markup).toContain('type="button">List folders');
    expect(markup).toContain('type="button">Open selected folder');
    expect(markup).toContain('<option value="">Drive root</option>');
    expect(markup).toContain('<option value="01REPORTS" selected="">Reports</option>');
    expect(markup).toContain("Leave blank for the root of the drive.");
  });

  it("falls back to a pasted folder ID for a drive Dex Web cannot browse", () => {
    const markup = renderUnit({target: unitTarget("folderPicker", {value: {driveId: "D4648F06C91D9D3D"}})});
    expect(markup).toContain("This drive ID cannot be browsed from Dex Web. Paste a folder ID instead.");
  });

  it("requires a folder only when the Flow marks the unit required", () => {
    const markup = renderUnit({target: unitTarget("folderPicker", {required: true, value: {driveId: "b!lib_1"}})});
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });
});
