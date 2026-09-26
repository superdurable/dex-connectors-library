import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { useState, type ReactNode } from "react";

export interface SpreadsheetUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  tabs: string[];
  busy?: boolean;
  onChooseSpreadsheet(): void;
  onLoadTabs(spreadsheetId: string): void;
  onSave(value: Record<string, unknown>): void;
}

export function SpreadsheetConfigurationUnit(props: SpreadsheetUnitProps) {
  if (props.target.unitId === "spreadsheetPicker") return <SpreadsheetPickerUnit {...props}/>;
  if (props.target.unitId === "sheetTabPicker") return <SheetTabPickerUnit {...props}/>;
  if (props.target.unitId === "textInput") return <TextInputUnit {...props}/>;
  const {target} = props;
  return <p role="alert">Unsupported Google Sheets configuration unit: {target.unitId}</p>;
}

function SpreadsheetPickerUnit({target, busy, onChooseSpreadsheet, onSave}: SpreadsheetUnitProps) {
  const [spreadsheetId, setSpreadsheetId] = useState(stringValue(target.value.spreadsheetId));
  const spreadsheetName = stringValue(target.value.spreadsheetName);
  return <UnitFrame target={target}>
    <button disabled={busy} onClick={onChooseSpreadsheet} type="button">Choose spreadsheet</button>
    {spreadsheetName && <p><strong>Spreadsheet:</strong> {spreadsheetName}</p>}
    <label>Spreadsheet ID<input value={spreadsheetId} onChange={(event) => setSpreadsheetId(event.target.value)}/></label>
    <button disabled={busy || (target.required && spreadsheetId.length === 0)} onClick={() => onSave({spreadsheetId, spreadsheetName})} type="button">Save</button>
  </UnitFrame>;
}

function SheetTabPickerUnit({target, busy, onLoadTabs, onSave, tabs}: SpreadsheetUnitProps) {
  const [tab, setTab] = useState(stringValue(target.value.tab));
  const spreadsheetId = stringValue(target.value.spreadsheetId);
  return <UnitFrame target={target}>
    <button disabled={busy || !spreadsheetId} onClick={() => onLoadTabs(spreadsheetId)} type="button">Load tabs</button>
    <label>Tab<select value={tab} onChange={(event) => setTab(event.target.value)}><option value="">Select a tab</option>{tabs.map((value) => <option key={value}>{value}</option>)}</select></label>
    <button disabled={busy || (target.required && tab.length === 0)} onClick={() => onSave({tab})} type="button">Save</button>
  </UnitFrame>;
}

function TextInputUnit({target, busy, onSave}: SpreadsheetUnitProps) {
  const [value, setValue] = useState(stringValue(target.value.text));
  return <UnitFrame target={target}><label>{target.label}<input value={value} onChange={(event) => setValue(event.target.value)}/></label><button disabled={busy || (target.required && value.length === 0)} onClick={() => onSave({text: value})} type="button">Save</button></UnitFrame>;
}

function UnitFrame({target, children}: {target: ConnectorStudioConfigurationUnitTarget; children: ReactNode}) {
  return <section className="unit"><h2>{target.label}</h2>{target.description && <p>{target.description}</p>}{children}</section>;
}

function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
