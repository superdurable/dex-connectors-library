// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StudioButton,
  StudioField,
  StudioNotice,
  type ConnectorStudioConfigurationUnitTarget,
} from "@superdurable/dex-connectors-react";
import { useEffect, useState } from "react";
import { TablePickerUnit, UnitFrame, WorksheetPickerUnit, SaveAction, stringValue } from "./contents-units.js";
import { graphItemIDPattern, type ExcelTable, type ExcelWorkbook, type ExcelWorksheet } from "./provider.js";

export interface ExcelUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  workbooks?: ExcelWorkbook[];
  /** isWorkbookListTruncated reports that paging stopped before Graph's last search page. */
  isWorkbookListTruncated?: boolean;
  /** resolvedWorkbook is the workbook the last pasted sharing link opened. */
  resolvedWorkbook?: ExcelWorkbook;
  worksheets?: ExcelWorksheet[];
  tables?: ExcelTable[];
  busy?: boolean;
  loadError?: string;
  onSearchWorkbooks(): void;
  onResolveSharingLink(link: string): void;
  onLoadWorksheets(driveId: string, workbookId: string): void;
  onLoadTables(driveId: string, workbookId: string): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function ExcelConfigurationUnit(props: ExcelUnitProps) {
  if (props.target.unitId === "workbookPicker") return <WorkbookPickerUnit {...props}/>;
  if (props.target.unitId === "worksheetPicker") return <WorksheetPickerUnit {...props}/>;
  if (props.target.unitId === "tablePicker") return <TablePickerUnit {...props}/>;
  return <StudioNotice tone="error">Unsupported Microsoft Excel configuration unit: {props.target.unitId}</StudioNotice>;
}

function WorkbookPickerUnit({target, workbooks = [], isWorkbookListTruncated, resolvedWorkbook, busy, loadError, onSearchWorkbooks, onResolveSharingLink, onSave}: ExcelUnitProps) {
  const [driveId, setDriveId] = useState(stringValue(target.value.driveId));
  const [workbookId, setWorkbookId] = useState(stringValue(target.value.workbookId));
  const [workbookName, setWorkbookName] = useState(stringValue(target.value.workbookName));
  const [sharingLink, setSharingLink] = useState("");
  useEffect(() => {
    if (!resolvedWorkbook) return;
    setDriveId(resolvedWorkbook.driveId);
    setWorkbookId(resolvedWorkbook.workbookId);
    setWorkbookName(resolvedWorkbook.name);
  }, [resolvedWorkbook]);
  const isValid = graphItemIDPattern.test(driveId) && graphItemIDPattern.test(workbookId);
  const selectedKey = `${driveId}/${workbookId}`;
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onSearchWorkbooks}>Search my workbooks</StudioButton>
      {workbookName && <span className="studio-muted"><strong>Workbook:</strong> {workbookName}</span>}
    </div>
    {workbooks.length > 0 && <StudioField label="Workbook">
      <select onChange={(event) => {
        const picked = workbooks.find((workbook) => `${workbook.driveId}/${workbook.workbookId}` === event.target.value);
        setDriveId(picked?.driveId ?? "");
        setWorkbookId(picked?.workbookId ?? "");
        setWorkbookName(picked?.name ?? "");
      }} value={selectedKey}>
        <option value="/">Select a workbook</option>
        {workbooks.map((workbook) => <option key={`${workbook.driveId}/${workbook.workbookId}`} value={`${workbook.driveId}/${workbook.workbookId}`}>{workbook.name}</option>)}
      </select>
    </StudioField>}
    {isWorkbookListTruncated && <StudioNotice tone="attention">
      The search found more workbooks than one list can show. Paste the workbook's sharing link to use one that is not listed.
    </StudioNotice>}
    <StudioField hint="For a workbook in SharePoint, Teams, or someone else's OneDrive: open it in Excel for the web, choose Share > Copy link, and paste the https:// link." label="Sharing link">
      <input onChange={(event) => setSharingLink(event.target.value)} type="url" value={sharingLink}/>
    </StudioField>
    <div className="studio-actions">
      <StudioButton disabled={busy || sharingLink.trim() === ""} onClick={() => onResolveSharingLink(sharingLink)}>Find workbook</StudioButton>
    </div>
    <StudioField hint="Advanced: the Microsoft Graph drive ID, such as b! followed by letters and digits, from the workbook's parentReference.driveId." label="Drive ID">
      <input onChange={(event) => { setDriveId(event.target.value.trim()); setWorkbookName(""); }} type="text" value={driveId}/>
    </StudioField>
    <StudioField hint="Advanced: the workbook's Microsoft Graph drive item ID, such as 01BYE5RZ6QN3ZWBTUFOFD3GSPGOHDJD36K." label="Workbook item ID">
      <input onChange={(event) => { setWorkbookId(event.target.value.trim()); setWorkbookName(""); }} type="text" value={workbookId}/>
    </StudioField>
    {(driveId !== "" || workbookId !== "") && !isValid && <StudioNotice tone="error">A drive ID and a workbook item ID use only letters, digits, and ! _ . ~ -.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave({driveId, workbookId, workbookName})}/>
  </UnitFrame>;
}
