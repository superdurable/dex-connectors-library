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
import { canListWorkbookContents } from "./provider.js";
import type { ExcelUnitProps } from "./units.js";

interface WorkbookContentItem { id: string; name: string; }

interface WorkbookContentPickerProps {
  props: ExcelUnitProps;
  noun: "worksheet" | "table";
  items: WorkbookContentItem[];
  outputPort: string;
  namePort: string;
  onLoad(driveId: string, workbookId: string): void;
}

export function WorksheetPickerUnit(props: ExcelUnitProps) {
  return <WorkbookContentPicker items={props.worksheets ?? []} namePort="worksheetName" noun="worksheet" onLoad={props.onLoadWorksheets} outputPort="worksheet" props={props}/>;
}

export function TablePickerUnit(props: ExcelUnitProps) {
  return <WorkbookContentPicker items={props.tables ?? []} namePort="tableName" noun="table" onLoad={props.onLoadTables} outputPort="table" props={props}/>;
}

// WorkbookContentPicker stores a listed item's stable ID, or a typed name, plus the display name.
function WorkbookContentPicker({props, noun, items, outputPort, namePort, onLoad}: WorkbookContentPickerProps) {
  const {target, busy, loadError, onSave} = props;
  const [reference, setReference] = useState(stringValue(target.value[outputPort]));
  const [displayName, setDisplayName] = useState(stringValue(target.value[namePort]));
  const driveId = stringValue(target.value.driveId);
  const workbookId = stringValue(target.value.workbookId);
  const hasWorkbook = driveId !== "" && workbookId !== "";
  const canList = canListWorkbookContents(driveId, workbookId);
  const isListed = items.some((item) => item.id === reference);
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy || !canList} onClick={() => onLoad(driveId, workbookId)}>{noun === "worksheet" ? "Load worksheets" : "Load tables"}</StudioButton>
      {!hasWorkbook && <span className="studio-muted">Save a workbook first to list its {noun}s.</span>}
      {displayName && <span className="studio-muted"><strong>{noun === "worksheet" ? "Worksheet:" : "Table:"}</strong> {displayName}</span>}
    </div>
    {hasWorkbook && !canList && <StudioNotice tone="attention">
      This workbook's drive cannot be listed here. Type the {noun} name as Excel shows it instead.
    </StudioNotice>}
    {items.length > 0 && <StudioField label={noun === "worksheet" ? "Worksheet" : "Table"}>
      <select onChange={(event) => {
        setReference(event.target.value);
        setDisplayName(items.find((item) => item.id === event.target.value)?.name ?? "");
      }} value={isListed ? reference : ""}>
        <option value="">{noun === "worksheet" ? "Select a worksheet" : "Select a table"}</option>
        {items.map((item) => <option key={item.id} value={item.id}>{item.name}</option>)}
      </select>
    </StudioField>}
    <StudioField hint={noun === "worksheet"
      ? "Or type the worksheet name exactly as its tab shows it, such as Summary; a picked worksheet is stored by ID so a rename keeps working."
      : "Or type the table name from Excel's Table Design > Table Name box, such as Decisions; a picked table is stored by ID so a rename keeps working."}
      label={noun === "worksheet" ? "Worksheet name" : "Table name"}>
      <input onChange={(event) => { setReference(event.target.value.trim()); setDisplayName(event.target.value.trim()); }} type="text" value={isListed ? displayName : reference}/>
    </StudioField>
    <SaveAction disabled={busy || reference === ""} onSave={() => onSave({[outputPort]: reference, [namePort]: displayName})}/>
  </UnitFrame>;
}

export function UnitFrame({target, loadError, children}: {target: ConnectorStudioConfigurationUnitTarget; loadError?: string; children: ReactNode}) {
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    {children}
  </StudioSurface>;
}

export function SaveAction({disabled, onSave}: {disabled?: boolean; onSave(): Promise<unknown> | void}) {
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

export function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
