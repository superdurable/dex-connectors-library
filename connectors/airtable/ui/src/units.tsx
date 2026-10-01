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
import { baseIDPattern, tableIDPattern, type AirtableBase, type AirtableTable } from "./provider.js";

export interface AirtableUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  bases?: AirtableBase[];
  /** isBaseListTruncated reports that paging stopped before Airtable's last base page. */
  isBaseListTruncated?: boolean;
  tables?: AirtableTable[];
  busy?: boolean;
  loadError?: string;
  onLoadBases(): void;
  onLoadTables(baseId: string): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function AirtableConfigurationUnit(props: AirtableUnitProps) {
  if (props.target.unitId === "basePicker") return <BasePickerUnit {...props}/>;
  if (props.target.unitId === "tablePicker") return <TablePickerUnit {...props}/>;
  const {target} = props;
  return <StudioNotice tone="error">Unsupported Airtable configuration unit: {target.unitId}</StudioNotice>;
}

function BasePickerUnit({target, bases = [], busy, isBaseListTruncated, loadError, onLoadBases, onSave}: AirtableUnitProps) {
  const [baseId, setBaseId] = useState(stringValue(target.value.baseId));
  const [baseName, setBaseName] = useState(stringValue(target.value.baseName));
  const isValidBaseID = baseIDPattern.test(baseId);
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadBases}>Load bases</StudioButton>
      {baseName && <span className="studio-muted"><strong>Base:</strong> {baseName}</span>}
    </div>
    {bases.length > 0 && <StudioField label="Base">
      <select onChange={(event) => {
        setBaseId(event.target.value);
        setBaseName(bases.find((base) => base.id === event.target.value)?.name ?? "");
      }} value={baseId}>
        <option value="">Select a base</option>
        {bases.map((base) => <option key={base.id} value={base.id}>{base.name}</option>)}
      </select>
    </StudioField>}
    {isBaseListTruncated && <StudioNotice tone="attention">
      Airtable returned more bases than one list can show. Enter a base ID to use one that is not listed.
    </StudioNotice>}
    <StudioField hint="Use a base ID when the base is not listed: app followed by 14 letters and digits, the part of the base's airtable.com address after airtable.com/." label="Base ID">
      <input onChange={(event) => { setBaseId(event.target.value.trim()); setBaseName(""); }} type="text" value={baseId}/>
    </StudioField>
    {baseId !== "" && !isValidBaseID && <StudioNotice tone="error">A base ID starts with app followed by 14 letters and digits.</StudioNotice>}
    <SaveAction disabled={busy || !isValidBaseID} onSave={() => onSave({baseId, baseName})}/>
  </UnitFrame>;
}

function TablePickerUnit({target, tables = [], busy, loadError, onLoadTables, onSave}: AirtableUnitProps) {
  const [tableId, setTableId] = useState(stringValue(target.value.tableId));
  const [tableName, setTableName] = useState(stringValue(target.value.tableName));
  const baseId = stringValue(target.value.baseId);
  const isValidTableID = tableIDPattern.test(tableId);
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy || !baseIDPattern.test(baseId)} onClick={() => onLoadTables(baseId)}>Load tables</StudioButton>
      {!baseId && <span className="studio-muted">Save a base first to list its tables.</span>}
      {tableName && <span className="studio-muted"><strong>Table:</strong> {tableName}</span>}
    </div>
    {tables.length > 0 && <StudioField label="Table">
      <select onChange={(event) => {
        setTableId(event.target.value);
        setTableName(tables.find((table) => table.id === event.target.value)?.name ?? "");
      }} value={tableId}>
        <option value="">Select a table</option>
        {tables.map((table) => <option key={table.id} value={table.id}>{table.name}</option>)}
      </select>
    </StudioField>}
    <StudioField hint="Use a table ID when the list is unavailable: tbl followed by 14 letters and digits, shown in the table's airtable.com address." label="Table ID">
      <input onChange={(event) => { setTableId(event.target.value.trim()); setTableName(""); }} type="text" value={tableId}/>
    </StudioField>
    {tableId !== "" && !isValidTableID && <StudioNotice tone="error">A table ID starts with tbl followed by 14 letters and digits.</StudioNotice>}
    <SaveAction disabled={busy || !isValidTableID} onSave={() => onSave({tableId, tableName})}/>
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
