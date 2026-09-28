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
import type { SpreadsheetFile } from "./provider.js";

export interface SpreadsheetUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  spreadsheets?: SpreadsheetFile[];
  /** isSpreadsheetListTruncated reports that paging stopped before Drive's last page. */
  isSpreadsheetListTruncated?: boolean;
  tabs: string[];
  busy?: boolean;
  loadError?: string;
  onChooseSpreadsheet(): void;
  onLoadTabs(spreadsheetId: string): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function SpreadsheetConfigurationUnit(props: SpreadsheetUnitProps) {
  if (props.target.unitId === "spreadsheetPicker") return <SpreadsheetPickerUnit {...props}/>;
  if (props.target.unitId === "sheetTabPicker") return <SheetTabPickerUnit {...props}/>;
  if (props.target.unitId === "textInput") return <TextInputUnit {...props}/>;
  const {target} = props;
  return <StudioNotice tone="error">Unsupported Google Sheets configuration unit: {target.unitId}</StudioNotice>;
}

function SpreadsheetPickerUnit({target, busy, isSpreadsheetListTruncated, loadError, onChooseSpreadsheet, onSave, spreadsheets = []}: SpreadsheetUnitProps) {
  const [spreadsheetId, setSpreadsheetId] = useState(stringValue(target.value.spreadsheetId));
  const [spreadsheetName, setSpreadsheetName] = useState(stringValue(target.value.spreadsheetName));
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onChooseSpreadsheet}>Choose spreadsheet</StudioButton>
      {spreadsheetName && <span className="studio-muted"><strong>Spreadsheet:</strong> {spreadsheetName}</span>}
    </div>
    {spreadsheets.length > 0 && <StudioField label="Spreadsheet">
      <select onChange={(event) => {
        setSpreadsheetId(event.target.value);
        setSpreadsheetName(spreadsheets.find((file) => file.id === event.target.value)?.name ?? "");
      }} value={spreadsheetId}>
        <option value="">Select a spreadsheet</option>
        {spreadsheets.map((file) => <option key={file.id} value={file.id}>{file.name}</option>)}
      </select>
    </StudioField>}
    {isSpreadsheetListTruncated && <StudioNotice tone="attention">
      Google Drive returned more spreadsheets than one list can show. Enter a spreadsheet ID to use one that is not listed.
    </StudioNotice>}
    <StudioField hint="Use a spreadsheet ID when the spreadsheet is not listed." label="Spreadsheet ID">
      <input onChange={(event) => setSpreadsheetId(event.target.value)} type="text" value={spreadsheetId}/>
    </StudioField>
    <SaveAction disabled={busy || (target.required && spreadsheetId.length === 0)} onSave={() => onSave({spreadsheetId, spreadsheetName})}/>
  </UnitFrame>;
}

function SheetTabPickerUnit({target, busy, loadError, onLoadTabs, onSave, tabs}: SpreadsheetUnitProps) {
  const [tab, setTab] = useState(stringValue(target.value.tab));
  const spreadsheetId = stringValue(target.value.spreadsheetId);
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy || !spreadsheetId} onClick={() => onLoadTabs(spreadsheetId)}>Load tabs</StudioButton>
      {!spreadsheetId && <span className="studio-muted">Save a spreadsheet first to list its tabs.</span>}
    </div>
    <StudioField label="Tab">
      <select onChange={(event) => setTab(event.target.value)} value={tab}>
        <option value="">Select a tab</option>
        {tabs.map((value) => <option key={value}>{value}</option>)}
      </select>
    </StudioField>
    <SaveAction disabled={busy || (target.required && tab.length === 0)} onSave={() => onSave({tab})}/>
  </UnitFrame>;
}

function TextInputUnit({target, busy, onSave}: SpreadsheetUnitProps) {
  const [value, setValue] = useState(stringValue(target.value.text));
  return <UnitFrame target={target}>
    <StudioField label={target.label}><input onChange={(event) => setValue(event.target.value)} type="text" value={value}/></StudioField>
    <SaveAction disabled={busy || (target.required && value.length === 0)} onSave={() => onSave({text: value})}/>
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
