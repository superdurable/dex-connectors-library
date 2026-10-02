// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";
import { isFolderReference, mailFolderCommands, mergeFolders, parseMailFolderPage } from "./provider.js";

describe("Microsoft Graph mail folder responses", () => {
  it("keeps named folders with usable IDs, builds their paths, and derives the next $skip", () => {
    expect(parseMailFolderPage({
      "@odata.context": "https://graph.microsoft.com/v1.0/$metadata#users('x')/mailFolders",
      value: [
        {id: "AAMkAGVmMDEzMTM4LTZmYWUtNDdkNC1hMDZiLTU1OGY5OTZhYmY4OAAuAAAAAAAiQ8W967B7TKBjgx9rVEURAQAiIsqMbYjsT5e-T7KzowPTAAAAAAEMAAA=",
          displayName: "Inbox", childFolderCount: 2},
        {id: "AAMk-archive_1=", displayName: "Archive", childFolderCount: 0},
        {id: "has/slash", displayName: "Rejected ID"},
        {id: "AAMk-noname="},
        {id: 7, displayName: "Numeric ID"},
      ],
      "@odata.nextLink": "https://graph.microsoft.com/v1.0/me/mailFolders?$skip=105",
    }, "", 100)).toEqual({
      folders: [
        {id: "AAMkAGVmMDEzMTM4LTZmYWUtNDdkNC1hMDZiLTU1OGY5OTZhYmY4OAAuAAAAAAAiQ8W967B7TKBjgx9rVEURAQAiIsqMbYjsT5e-T7KzowPTAAAAAAEMAAA=",
          displayName: "Inbox", childFolderCount: 2, path: "Inbox"},
        {id: "AAMk-archive_1=", displayName: "Archive", childFolderCount: 0, path: "Archive"},
      ],
      nextSkip: "105",
    });
    expect(parseMailFolderPage({value: [{id: "AAMk-support=", displayName: "Support"}]}, "Inbox", 0))
      .toEqual({folders: [{id: "AAMk-support=", displayName: "Support", childFolderCount: 0, path: "Inbox / Support"}], nextSkip: ""});
  });

  it("reports a Graph error object in connector words, never Graph's text", () => {
    const graphError = {error: {code: "ErrorAccessDenied", message: "secret detail from Graph"}};
    expect(() => parseMailFolderPage(graphError, "", 0)).toThrow("Microsoft Graph returned an error instead of the folder list");
    expect(() => parseMailFolderPage(graphError, "", 0)).not.toThrow("secret detail");
  });

  it("lists /me folders for OAuth and the configured mailbox's folders for app-only", () => {
    const delegated = mailFolderCommands({state: "connected", grantedScopes: [], authMethodIds: ["microsoft-oauth"], configuration: {}});
    expect(delegated.isAvailable && delegated.topLevel("")).toEqual({commandId: "listMailFolders", parameters: {}});
    expect(delegated.isAvailable && delegated.children("AAMk-inbox=", "100"))
      .toEqual({commandId: "listChildMailFolders", parameters: {folderId: "AAMk-inbox=", skip: "100"}});
    const appOnly = mailFolderCommands({state: "connected", grantedScopes: [], authMethodIds: ["app-only"], configuration: {mailbox: " support@contoso.com "}});
    expect(appOnly.isAvailable && appOnly.topLevel("")).toEqual({commandId: "listMailboxMailFolders", parameters: {mailbox: "support@contoso.com"}});
    expect(appOnly.isAvailable && appOnly.children("AAMk-inbox=", ""))
      .toEqual({commandId: "listMailboxChildMailFolders", parameters: {mailbox: "support@contoso.com", folderId: "AAMk-inbox="}});
    expect(mailFolderCommands({state: "connected", grantedScopes: [], authMethodIds: ["app-only"]}))
      .toEqual({isAvailable: false, reason: "Save the connection's mailbox field first; app-only folders are listed for that mailbox."});
    const olderHost = mailFolderCommands({state: "connected", grantedScopes: []});
    expect(olderHost.isAvailable && olderHost.topLevel("").commandId).toBe("listMailFolders");
  });

  it("accepts folder IDs and well-known names as the manual fallback", () => {
    expect(isFolderReference("archive")).toBe(true);
    expect(isFolderReference("DeletedItems")).toBe(true);
    expect(isFolderReference("AAMk-archive_1=")).toBe(true);
    expect(isFolderReference("Inbox/Support")).toBe(false);
    expect(isFolderReference("inbox?$top=999")).toBe(false);
  });

  it("merges a parent's subfolders after the folders already listed", () => {
    const inbox = {id: "a", displayName: "Inbox", childFolderCount: 1, path: "Inbox"};
    const support = {id: "b", displayName: "Support", childFolderCount: 0, path: "Inbox / Support"};
    expect(mergeFolders([inbox], [inbox, support])).toEqual([inbox, support]);
  });
});
