// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StudioButton,
  StudioField,
  StudioHeader,
  StudioNotice,
  StudioSurface,
  type ConnectorStudioConfigurationUnitTarget,
} from "@superdurable/dex-connectors-react";
import { useState } from "react";
import { isFolderReference, type MailFolder } from "./provider.js";

export interface MailFolderUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  folders: MailFolder[];
  /** isFolderListTruncated reports that paging stopped before Graph's last page. */
  isFolderListTruncated?: boolean;
  busy?: boolean;
  loadError?: string;
  onLoadFolders(): void;
  onLoadChildFolders(folder: MailFolder): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function OutlookMailConfigurationUnit(props: MailFolderUnitProps) {
  if (props.target.unitId === "mailFolderPicker") return <MailFolderPickerUnit {...props}/>;
  return <StudioNotice tone="error">Unsupported Outlook Mail configuration unit: {props.target.unitId}</StudioNotice>;
}

function MailFolderPickerUnit({target, folders, isFolderListTruncated, busy, loadError, onLoadFolders, onLoadChildFolders, onSave}: MailFolderUnitProps) {
  const [folderId, setFolderId] = useState(stringValue(target.value.folderId));
  const [folderName, setFolderName] = useState(stringValue(target.value.folderName));
  const trimmedFolderId = folderId.trim();
  const isValid = trimmedFolderId === "" ? !target.required : isFolderReference(trimmedFolderId);
  const selected = folders.find((folder) => folder.id === folderId);
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadFolders}>Load folders</StudioButton>
      {selected && selected.childFolderCount > 0 &&
        <StudioButton disabled={busy} onClick={() => onLoadChildFolders(selected)}>Show subfolders of {selected.displayName}</StudioButton>}
      {folderName && <span className="studio-muted"><strong>Folder:</strong> {folderName}</span>}
    </div>
    {folders.length > 0 && <StudioField label="Mail folder">
      <select onChange={(event) => {
        setFolderId(event.target.value);
        setFolderName(folders.find((folder) => folder.id === event.target.value)?.path ?? "");
      }} value={folderId}>
        <option value="">{target.required ? "Select a folder" : "Use the Step's default folder"}</option>
        {folders.map((folder) => <option key={folder.id} value={folder.id}>
          {folder.path}{folder.childFolderCount > 0 ? ` (${folder.childFolderCount} subfolders)` : ""}
        </option>)}
      </select>
    </StudioField>}
    {isFolderListTruncated && <StudioNotice tone="attention">
      Only the first folders are listed. Enter the folder ID below to use one that is not listed.
    </StudioNotice>}
    <StudioField
      hint="Use a folder ID from Microsoft Graph's mailFolders list, or a well-known name such as archive, inbox, or deleteditems, when the folder is not listed. Subfolders appear after Show subfolders."
      label="Folder ID fallback">
      <input onChange={(event) => { setFolderId(event.target.value); setFolderName(""); }} placeholder="archive" type="text" value={folderId}/>
    </StudioField>
    {!isValid && trimmedFolderId !== "" && <StudioNotice tone="error">Enter a Graph folder ID or a well-known folder name such as archive.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave(trimmedFolderId === "" ? {} : {folderId: trimmedFolderId, folderName})}/>
  </StudioSurface>;
}

function SaveAction({disabled, onSave}: {disabled?: boolean; onSave(): Promise<unknown> | void}) {
  const [saveState, setSaveState] = useState<{status: "idle" | "saved"} | {status: "failed"; message: string}>({status: "idle"});
  const save = () => {
    Promise.resolve(onSave()).then(
      () => setSaveState({status: "saved"}),
      (error: unknown) => setSaveState({status: "failed", message: error instanceof Error ? error.message : "Unknown error"}),
    );
  };
  return <>
    <div className="studio-actions"><StudioButton disabled={disabled} onClick={save} variant="primary">Save</StudioButton></div>
    {saveState.status === "saved" && <StudioNotice tone="success">Saved. Restart the application to use it.</StudioNotice>}
    {saveState.status === "failed" && <StudioNotice tone="error">The configuration could not be saved: {saveState.message}</StudioNotice>}
  </>;
}

function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
