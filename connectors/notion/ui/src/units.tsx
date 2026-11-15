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
import { parseNotionID, type NotionDataSource } from "./provider.js";

export interface NotionUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  dataSources: NotionDataSource[];
  busy?: boolean;
  loadError?: string;
  onLoadDataSources(databaseLinkOrID: string): void;
  onCheckDataSource(dataSourceID: string): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function NotionConfigurationUnit(props: NotionUnitProps) {
  switch (props.target.unitId) {
    case "dataSourcePicker": return <DataSourcePickerUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported Notion configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function DataSourcePickerUnit({target, dataSources, busy, loadError, onLoadDataSources, onCheckDataSource, onSave}: NotionUnitProps) {
  const [databaseLink, setDatabaseLink] = useState("");
  const [dataSourceID, setDataSourceID] = useState(stringValue(target.value.dataSourceId));
  const savedName = stringValue(target.value.dataSourceName);
  const parsedID = parseNotionID(dataSourceID);
  const listedName = dataSources.find((dataSource) => dataSource.id === parsedID)?.name;
  const isValid = parsedID !== "";
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <StudioField hint="In Notion, open the database's ••• menu > Copy link, such as https://www.notion.so/Requests-2a4d6f801b3c4d5e8f70112233445500?v=… The database must be shared with this connection." label="Database link or ID">
      <input onChange={(event) => setDatabaseLink(event.target.value)} placeholder="https://www.notion.so/…" type="text" value={databaseLink}/>
    </StudioField>
    <div className="studio-actions">
      <StudioButton disabled={busy || parseNotionID(databaseLink) === ""} onClick={() => onLoadDataSources(databaseLink)}>Load data sources</StudioButton>
      <span className="studio-muted">Lists the database's data sources live from Notion.</span>
    </div>
    <StudioField label="Data source">
      <select onChange={(event) => setDataSourceID(event.target.value)} value={listedName ? parsedID : ""}>
        <option value="">{dataSources.length === 0 ? "Load a database first" : "Select a data source"}</option>
        {dataSources.map((dataSource) => <option key={dataSource.id} value={dataSource.id}>{dataSource.name} · {dataSource.id}</option>)}
      </select>
    </StudioField>
    <StudioField hint="Or paste a data source ID from the database's settings > Manage data sources > Copy data source ID: 32 hexadecimal characters, with or without dashes." label="Data source ID">
      <input onChange={(event) => setDataSourceID(event.target.value.trim())} placeholder="2a4d6f80-1b3c-4d5e-8f70-112233445566" type="text" value={dataSourceID}/>
    </StudioField>
    <div className="studio-actions">
      <StudioButton disabled={busy || !isValid} onClick={() => onCheckDataSource(parsedID)}>Check data source</StudioButton>
      {savedName && <span className="studio-muted">Saved: {savedName}</span>}
    </div>
    {!isValid && dataSourceID !== "" && <StudioNotice tone="error">Enter a data source ID of 32 hexadecimal characters, with or without dashes.</StudioNotice>}
    {!isValid && dataSourceID === "" && <StudioNotice tone="attention">No data source is saved; the Trigger cannot start until one is.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave({dataSourceId: parsedID, dataSourceName: listedName ?? savedName})}/>
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
