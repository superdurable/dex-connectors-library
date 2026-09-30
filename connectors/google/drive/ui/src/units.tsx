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
import { useState, type ReactNode } from "react";
import { folderIDFromInput, type DriveFolder } from "./provider.js";

export interface DriveUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  folders?: DriveFolder[];
  /** isFolderListTruncated reports that paging stopped before Drive's last page. */
  isFolderListTruncated?: boolean;
  busy?: boolean;
  loadError?: string;
  onChooseFolder(): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function DriveConfigurationUnit(props: DriveUnitProps) {
  if (props.target.unitId === "folderPicker") return <FolderPickerUnit {...props}/>;
  const {target} = props;
  return <StudioNotice tone="error">Unsupported Google Drive configuration unit: {target.unitId}</StudioNotice>;
}

function FolderPickerUnit({target, busy, folders = [], isFolderListTruncated, loadError, onChooseFolder, onSave}: DriveUnitProps) {
  const [folderInput, setFolderInput] = useState(stringValue(target.value.folderId));
  const [folderName, setFolderName] = useState(stringValue(target.value.folderName));
  const folderId = folderIDFromInput(folderInput);
  const isFolderInputInvalid = folderInput.trim() !== "" && folderId === "";
  const chooseListedFolder = (id: string) => {
    setFolderInput(id);
    setFolderName(folders.find((folder) => folder.id === id)?.name ?? "");
  };
  const editFolderInput = (value: string) => {
    setFolderInput(value);
    const listedFolder = folders.find((folder) => folder.id === folderIDFromInput(value));
    setFolderName(listedFolder?.name ?? "");
  };
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onChooseFolder}>Choose folder</StudioButton>
      {folderName && <span className="studio-muted"><strong>Folder:</strong> {folderName}</span>}
    </div>
    {folders.length > 0 && <StudioField label="Folder">
      <select onChange={(event) => chooseListedFolder(event.target.value)} value={folderId}>
        <option value="">{target.required ? "Select a folder" : "No folder"}</option>
        {folders.map((folder) => <option key={folder.id} value={folder.id}>{folder.name}</option>)}
      </select>
    </StudioField>}
    {isFolderListTruncated && <StudioNotice tone="attention">
      Google Drive returned more folders than one list can show. Enter a folder ID to use one that is not listed.
    </StudioNotice>}
    <StudioField hint="Paste a folder ID or its drive.google.com/drive/folders link when the folder is not listed. Leave blank for no folder." label="Folder ID">
      <input onChange={(event) => editFolderInput(event.target.value)} type="text" value={folderInput}/>
    </StudioField>
    {isFolderInputInvalid && <StudioNotice tone="error">Enter a Drive folder ID or a drive.google.com/drive/folders link.</StudioNotice>}
    <SaveAction
      disabled={busy || isFolderInputInvalid || (target.required && folderId === "")}
      onSave={() => onSave({folderId, folderName: folderId === "" ? "" : folderName})}
    />
  </UnitFrame>;
}

function UnitFrame({target, loadError, children}: {target: ConnectorStudioConfigurationUnitTarget; loadError?: string; children: ReactNode}) {
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    {children}
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
