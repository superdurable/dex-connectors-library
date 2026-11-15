// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { DriveSetupView } from "../src/setup.js";
import { DriveConfigurationUnit } from "../src/units.js";

function folderPickerTarget(overrides: Partial<ConnectorStudioConfigurationUnitTarget> = {}): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit",
    scope: {kind: "operation", operationId: "uploadFile", flowType: "GoogleDriveTextCopy", stepType: "UploadTextCopy"},
    instanceId: "destinationFolder",
    unitId: "folderPicker",
    label: "Destination folder",
    description: "Choose the folder that receives the text copy; leave blank to create the copy in the My Drive root.",
    required: false,
    bindings: [{port: "folderId", jsonPointer: "/folderId"}, {port: "folderName", jsonPointer: "/folderName"}],
    value: {},
    ...overrides,
  };
}

describe("Google Drive setup", () => {
  it("shows the connected account without credential material", () => {
    const markup = renderToStaticMarkup(<DriveSetupView connection={{state: "connected", accountEmail: "owner@example.com", grantedScopes: ["https://www.googleapis.com/auth/drive.readonly", "https://www.googleapis.com/auth/drive.file"]}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain("owner@example.com");
    expect(markup).not.toContain("Choose folder");
    expect(markup).not.toContain("access_token");
    expect(markup).not.toContain("refresh_token");
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<DriveSetupView connection={{state, grantedScopes: []}} onConnect={() => undefined} onReconnect={() => undefined}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("Reconnect Google Drive");
  });

  it("renders every folder picker field with operation guidance and blank semantics", () => {
    const markup = renderToStaticMarkup(<DriveConfigurationUnit folders={[{id: "folder-1", name: "Finance"}]} onChooseFolder={() => undefined} onSave={() => undefined} target={folderPickerTarget({value: {folderId: "folder-1", folderName: "Finance"}})}/>);
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain("Destination folder");
    expect(markup).toContain("leave blank to create the copy in the My Drive root.");
    expect(markup).toContain(">Choose folder<");
    expect(markup).toContain("<strong>Folder:</strong> Finance");
    expect(markup).toContain('<option value="">No folder</option>');
    expect(markup).toContain('<option value="folder-1" selected="">Finance</option>');
    expect(markup).toContain("Paste a folder ID or its drive.google.com/drive/folders link when the folder is not listed. Leave blank for no folder.");
    expect(markup).toContain('value="folder-1"');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button">Save');
    expect(markup).not.toContain("access_token");
    expect(markup).not.toContain("<style");
  });

  it("requires a folder only when the Flow marks the unit required", () => {
    const optional = renderToStaticMarkup(<DriveConfigurationUnit onChooseFolder={() => undefined} onSave={() => undefined} target={folderPickerTarget()}/>);
    expect(optional).toContain('class="studio-button studio-button-primary" type="button">Save');
    const required = renderToStaticMarkup(<DriveConfigurationUnit folders={[{id: "folder-1", name: "Finance"}]} onChooseFolder={() => undefined} onSave={() => undefined} target={folderPickerTarget({required: true})}/>);
    expect(required).toContain('<option value="" selected="">Select a folder</option>');
    expect(required).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });

  it("rejects a folder value that is neither an ID nor a folder link", () => {
    const markup = renderToStaticMarkup(<DriveConfigurationUnit onChooseFolder={() => undefined} onSave={() => undefined} target={folderPickerTarget({value: {folderId: "not a folder"}})}/>);
    expect(markup).toContain('role="alert">Enter a Drive folder ID or a drive.google.com/drive/folders link.');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });

  it("points to the folder ID field when Drive returns more folders than the list shows", () => {
    const markup = renderToStaticMarkup(<DriveConfigurationUnit folders={[{id: "folder-1", name: "Finance"}]} isFolderListTruncated onChooseFolder={() => undefined} onSave={() => undefined} target={folderPickerTarget()}/>);
    expect(markup).toContain('role="status">Google Drive returned more folders than one list can show. Enter a folder ID to use one that is not listed.');
  });

  it("shows a folder list error inline", () => {
    const markup = renderToStaticMarkup(<DriveConfigurationUnit loadError="The caller does not have permission" onChooseFolder={() => undefined} onSave={() => undefined} target={folderPickerTarget()}/>);
    expect(markup).toContain('role="alert">The caller does not have permission');
  });

  it("renders the file type picker with Trigger guidance, the MIME type link, and blank semantics", () => {
    const markup = renderToStaticMarkup(<DriveConfigurationUnit onChooseFolder={() => undefined} onSave={() => undefined} target={mimeTypePickerTarget({value: {mimeType: "application/pdf"}})}/>);
    expect(markup).toContain("File type");
    expect(markup).toContain("leave blank to start a Flow for new files of every type, including folders.");
    expect(markup).toContain('<option value="">Any file type</option>');
    expect(markup).toContain('<option value="application/pdf" selected="">PDF</option>');
    expect(markup).toContain('<option value="application/vnd.google-apps.document">Google Docs</option>');
    expect(markup).toContain("https://developers.google.com/workspace/drive/api/guides/mime-types");
    expect(markup).toContain("Leave blank for every file type.");
    expect(markup).toContain('class="studio-button studio-button-primary" type="button">Save');
    expect(markup).not.toContain("access_token");
  });

  it("keeps an unlisted MIME type as a custom choice", () => {
    const markup = renderToStaticMarkup(<DriveConfigurationUnit onChooseFolder={() => undefined} onSave={() => undefined} target={mimeTypePickerTarget({value: {mimeType: "application/zip"}})}/>);
    expect(markup).toContain('<option value="custom" selected="">Custom: application/zip</option>');
  });

  it("rejects a MIME type with parameters or capitals", () => {
    const markup = renderToStaticMarkup(<DriveConfigurationUnit onChooseFolder={() => undefined} onSave={() => undefined} target={mimeTypePickerTarget({value: {mimeType: "Text/Plain; charset=utf-8"}})}/>);
    expect(markup).toContain('role="alert">Enter one lowercase MIME type such as application/pdf, without parameters.');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });

  it("requires a file type only when the Flow marks the unit required", () => {
    const markup = renderToStaticMarkup(<DriveConfigurationUnit onChooseFolder={() => undefined} onSave={() => undefined} target={mimeTypePickerTarget({required: true})}/>);
    expect(markup).toContain('<option value="" selected="">Select a file type</option>');
    expect(markup).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
  });
});

function mimeTypePickerTarget(overrides: Partial<ConnectorStudioConfigurationUnitTarget> = {}): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit",
    scope: {kind: "trigger", triggerName: "fileCreated", flowType: "GoogleDriveFileIntake", bindingName: "google-drive-file-intake-new-file"},
    instanceId: "mimeType",
    unitId: "mimeTypePicker",
    label: "File type",
    description: "Choose the exact file type that starts the intake Flow, such as PDF for scanned invoices; leave blank to start a Flow for new files of every type, including folders.",
    required: false,
    bindings: [{port: "mimeType", jsonPointer: "/mimeType"}],
    value: {},
    ...overrides,
  } as ConnectorStudioConfigurationUnitTarget;
}
