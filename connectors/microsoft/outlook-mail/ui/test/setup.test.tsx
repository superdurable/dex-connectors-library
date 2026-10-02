// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { OutlookMailSetupView } from "../src/setup.js";
import { OutlookMailConfigurationUnit } from "../src/units.js";

const sentinelToken = "SENTINEL-OUTLOOK-ACCESS-TOKEN";

const pickerTarget: ConnectorStudioConfigurationUnitTarget = {
  kind: "configurationUnit", scope: {kind: "operation", operationId: "moveMessage", flowType: "OutlookSupportReply", stepType: "ArchiveCustomerMessage"},
  instanceId: "archiveFolder", unitId: "mailFolderPicker", label: "Archive folder",
  description: "Choose the mail folder that receives each answered customer message. Leave it unsaved to use the mailbox's well-known Archive folder.",
  required: false,
  bindings: [{port: "folderId", jsonPointer: "/folderId"}, {port: "folderName", jsonPointer: "/folderName"}],
  value: {folderId: "AAMk-answered=", folderName: "Inbox / Answered"},
};

const noop = () => undefined;

describe("Microsoft Outlook Mail setup", () => {
  it("offers OAuth consent and shows the connected account without any credential", () => {
    const notConfigured = renderToStaticMarkup(<OutlookMailSetupView connection={{state: "not_configured", grantedScopes: []}} onConnect={noop} onReconnect={noop}/>);
    expect(notConfigured).toContain("Connect Microsoft Outlook");
    expect(notConfigured).toContain("multitenant Entra app");
    const connected = renderToStaticMarkup(<OutlookMailSetupView connection={{
      state: "connected", accountEmail: "support@contoso.com", grantedScopes: ["Mail.ReadWrite", "Mail.Send"], authMethodIds: ["microsoft-oauth"],
    }} onConnect={noop} onReconnect={noop}/>);
    expect(connected).toContain("<strong>support@contoso.com</strong>");
    for (const secretName of ["access_token", "refresh_token", "client_secret", sentinelToken]) expect(connected).not.toContain(secretName);
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<OutlookMailSetupView connection={{state, grantedScopes: []}} onConnect={noop} onReconnect={noop}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("accept Mail.ReadWrite, Mail.Send, and offline access");
    expect(markup).toContain("Reconnect Microsoft Outlook");
  });

  it("guides an app-only connection without OAuth consent", () => {
    const markup = renderToStaticMarkup(<OutlookMailSetupView connection={{state: "not_configured", grantedScopes: [], authMethodIds: ["app-only"]}}
      onConnect={noop} onReconnect={noop}/>);
    expect(markup).toContain("leave access_token blank");
    expect(markup).toContain("Exchange RBAC for Applications");
    expect(markup).not.toContain("Connect Microsoft Outlook");
  });

  it("renders the saved folder with the Step's guidance, the fallback, and no styles", () => {
    const markup = renderToStaticMarkup(<OutlookMailConfigurationUnit folders={[]} onLoadChildFolders={noop} onLoadFolders={noop} onSave={noop} target={pickerTarget}/>);
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain("Leave it unsaved to use the mailbox&#x27;s well-known Archive folder.");
    expect(markup).toContain("<strong>Folder:</strong> Inbox / Answered");
    expect(markup).toContain('value="AAMk-answered="');
    expect(markup).toContain('placeholder="archive"');
    expect(markup).toContain("a well-known name such as archive, inbox, or deleteditems");
    expect(markup).toContain('class="studio-button studio-button-primary" type="button">Save');
    expect(markup).not.toContain("<style");
    expect(markup).not.toContain(sentinelToken);
  });

  it("lists folders by path, offers subfolders of the selection, and points to manual entry when truncated", () => {
    const markup = renderToStaticMarkup(<OutlookMailConfigurationUnit
      folders={[
        {id: "AAMk-inbox=", displayName: "Inbox", childFolderCount: 2, path: "Inbox"},
        {id: "AAMk-answered=", displayName: "Answered", childFolderCount: 0, path: "Inbox / Answered"},
      ]}
      isFolderListTruncated
      onLoadChildFolders={noop} onLoadFolders={noop} onSave={noop} target={{...pickerTarget, value: {folderId: "AAMk-inbox="}}}/>);
    expect(markup).toContain("Inbox (2 subfolders)");
    expect(markup).toContain("Inbox / Answered");
    expect(markup).toContain("Use the Step&#x27;s default folder");
    expect(markup).toContain("Show subfolders of Inbox");
    expect(markup).toContain("Only the first folders are listed.");
  });

  it("shows a load error inline and requires a folder only for a required unit", () => {
    const markup = renderToStaticMarkup(<OutlookMailConfigurationUnit folders={[]} loadError="Mail folders could not be loaded."
      onLoadChildFolders={noop} onLoadFolders={noop} onSave={noop} target={{...pickerTarget, required: true, value: {}}}/>);
    expect(markup).toContain('role="alert">Mail folders could not be loaded.');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });

  it("rejects an unknown unit", () => {
    const markup = renderToStaticMarkup(<OutlookMailConfigurationUnit folders={[]} onLoadChildFolders={noop} onLoadFolders={noop} onSave={noop}
      target={{...pickerTarget, unitId: "textInput"}}/>);
    expect(markup).toContain("Unsupported Outlook Mail configuration unit: textInput");
  });
});
