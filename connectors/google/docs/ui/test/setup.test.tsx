// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";
import { DocsSetupView } from "../src/setup.js";
import { DocsConfigurationUnit } from "../src/units.js";

function templatePickerTarget(overrides: Partial<ConnectorStudioConfigurationUnitTarget> = {}): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit",
    scope: {kind: "operation", operationId: "getDocumentText", flowType: "GoogleDocsPolicyPublish", stepType: "ReadPolicyTemplate"},
    instanceId: "policyTemplate",
    unitId: "documentPicker",
    label: "Policy template",
    description: "Choose the Google Doc whose text, including its {{placeholders}}, every published policy copies; the Flow fails at its first Step until a template is saved.",
    required: true,
    bindings: [{port: "documentId", jsonPointer: "/documentId"}, {port: "documentTitle", jsonPointer: "/documentTitle"}],
    value: {},
    ...overrides,
  };
}

function folderPickerTarget(overrides: Partial<ConnectorStudioConfigurationUnitTarget> = {}): ConnectorStudioConfigurationUnitTarget {
  return {
    kind: "configurationUnit",
    scope: {kind: "operation", operationId: "createDocument", flowType: "GoogleDocsPolicyPublish", stepType: "CreatePolicyDocument"},
    instanceId: "destinationFolder",
    unitId: "folderPicker",
    label: "Destination folder",
    description: "Choose the Drive folder that receives each published policy; leave blank to create it in the My Drive root.",
    required: false,
    bindings: [{port: "folderId", jsonPointer: "/folderId"}, {port: "folderName", jsonPointer: "/folderName"}],
    value: {},
    ...overrides,
  };
}

const noop = () => undefined;

describe("Google Docs setup", () => {
  it("shows the connected account without credential material", () => {
    const markup = renderToStaticMarkup(<DocsSetupView connection={{state: "connected", accountEmail: "owner@example.com", grantedScopes: [
      "https://www.googleapis.com/auth/documents", "https://www.googleapis.com/auth/drive.file", "https://www.googleapis.com/auth/drive.metadata.readonly",
    ]}} onConnect={noop} onReconnect={noop}/>);
    expect(markup).toContain("owner@example.com");
    expect(markup).not.toContain("Choose document");
    expect(markup).not.toContain("access_token");
    expect(markup).not.toContain("refresh_token");
  });

  it.each([
    ["revoked", "Connection revoked"],
    ["insufficient_scope", "Additional permission required"],
  ] as const)("shows a reconnect action for %s", (state, label) => {
    const markup = renderToStaticMarkup(<DocsSetupView connection={{state, grantedScopes: []}} onConnect={noop} onReconnect={noop}/>);
    expect(markup).toContain(label);
    expect(markup).toContain("Reconnect Google Docs");
  });

  it("renders every document picker field with operation guidance", () => {
    const markup = renderToStaticMarkup(<DocsConfigurationUnit documents={[{id: "doc-1", title: "Refund Policy Template", modifiedTime: "2026-09-30T12:00:00Z"}]}
      onChooseDocument={noop} onChooseFolder={noop} onSave={noop} target={templatePickerTarget({value: {documentId: "doc-1", documentTitle: "Refund Policy Template"}})}/>);
    expect(markup).toContain('class="studio-surface"');
    expect(markup).toContain("Policy template");
    expect(markup).toContain("the Flow fails at its first Step until a template is saved.");
    expect(markup).toContain(">Choose document<");
    expect(markup).toContain("<strong>Document:</strong> Refund Policy Template");
    expect(markup).toContain('<option value="">Select a document</option>');
    expect(markup).toContain('<option value="doc-1" selected="">Refund Policy Template</option>');
    expect(markup).toContain("Paste a document ID or its docs.google.com/document link when the document is not listed. Leave blank for no document.");
    expect(markup).toContain('class="studio-button studio-button-primary" type="button">Save');
    expect(markup).not.toContain("access_token");
    expect(markup).not.toContain("<style");
  });

  it("requires a template before saving and rejects a value that is neither an ID nor a document link", () => {
    const blank = renderToStaticMarkup(<DocsConfigurationUnit onChooseDocument={noop} onChooseFolder={noop} onSave={noop} target={templatePickerTarget()}/>);
    expect(blank).toContain('class="studio-button studio-button-primary" type="button" disabled="">Save');
    const invalid = renderToStaticMarkup(<DocsConfigurationUnit onChooseDocument={noop} onChooseFolder={noop} onSave={noop}
      target={templatePickerTarget({value: {documentId: "https://docs.google.com/spreadsheets/d/abc/edit"}})}/>);
    expect(invalid).toContain('role="alert">Enter a Google Docs document ID or a docs.google.com/document link.');
  });

  it("renders every folder picker field with blank semantics", () => {
    const markup = renderToStaticMarkup(<DocsConfigurationUnit folders={[{id: "folder-1", name: "Policies"}]} onChooseDocument={noop} onChooseFolder={noop}
      onSave={noop} target={folderPickerTarget({value: {folderId: "folder-1", folderName: "Policies"}})}/>);
    expect(markup).toContain("Destination folder");
    expect(markup).toContain("leave blank to create it in the My Drive root.");
    expect(markup).toContain(">Choose folder<");
    expect(markup).toContain("<strong>Folder:</strong> Policies");
    expect(markup).toContain('<option value="">No folder</option>');
    expect(markup).toContain("Paste a folder ID or its drive.google.com/drive/folders link when the folder is not listed. Leave blank for no folder.");
    const optional = renderToStaticMarkup(<DocsConfigurationUnit onChooseDocument={noop} onChooseFolder={noop} onSave={noop} target={folderPickerTarget()}/>);
    expect(optional).toContain('class="studio-button studio-button-primary" type="button">Save');
  });

  it("points to the ID field when Drive returns more documents than the list shows, and shows list errors inline", () => {
    const truncated = renderToStaticMarkup(<DocsConfigurationUnit documents={[{id: "doc-1", title: "A", modifiedTime: ""}]} isDocumentListTruncated
      onChooseDocument={noop} onChooseFolder={noop} onSave={noop} target={templatePickerTarget()}/>);
    expect(truncated).toContain('role="status">Google Drive returned more documents than one list can show. Enter a document ID to use one that is not listed.');
    const failed = renderToStaticMarkup(<DocsConfigurationUnit loadError="The caller does not have permission" onChooseDocument={noop} onChooseFolder={noop}
      onSave={noop} target={templatePickerTarget()}/>);
    expect(failed).toContain('role="alert">The caller does not have permission');
  });
});
