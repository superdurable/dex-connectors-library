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
import type { CalendarListEntry } from "./provider.js";

export interface CalendarUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  calendars?: CalendarListEntry[];
  /** isCalendarListTruncated reports that Graph had more calendars than the one listed page. */
  isCalendarListTruncated?: boolean;
  busy?: boolean;
  loadError?: string;
  onChooseCalendar(): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function CalendarConfigurationUnit(props: CalendarUnitProps) {
  if (props.target.unitId === "calendarPicker") return <CalendarPickerUnit {...props}/>;
  return <StudioNotice tone="error">Unsupported Outlook Calendar configuration unit: {props.target.unitId}</StudioNotice>;
}

function CalendarPickerUnit({target, busy, calendars = [], isCalendarListTruncated, loadError, onChooseCalendar, onSave}: CalendarUnitProps) {
  const [calendarId, setCalendarId] = useState(stringValue(target.value.calendarId));
  const [calendarName, setCalendarName] = useState(stringValue(target.value.calendarName));
  const trimmedCalendarId = calendarId.trim();
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onChooseCalendar}>Choose calendar</StudioButton>
      {calendarName && <span className="studio-muted"><strong>Calendar:</strong> {calendarName}</span>}
    </div>
    {calendars.length > 0 && <StudioField label="Calendar">
      <select onChange={(event) => {
        setCalendarId(event.target.value);
        setCalendarName(calendars.find((calendar) => calendar.id === event.target.value)?.name ?? "");
      }} value={calendarId}>
        <option value="">Default calendar</option>
        {calendars.map((calendar) => <option key={calendar.id} value={calendar.id}>{describeCalendar(calendar)}</option>)}
      </select>
    </StudioField>}
    {isCalendarListTruncated && <StudioNotice tone="attention">
      Microsoft Graph returned more calendars than one list can show. Enter a calendar ID to use one that is not listed.
    </StudioNotice>}
    <StudioField hint="Leave blank for the mailbox's default calendar, or paste a calendar ID returned by Microsoft Graph GET /me/calendars when the calendar is not listed." label="Calendar ID">
      <input onChange={(event) => { setCalendarId(event.target.value); setCalendarName(""); }} placeholder="Default calendar" type="text" value={calendarId}/>
    </StudioField>
    <SaveAction disabled={busy || (target.required && trimmedCalendarId.length === 0)} onSave={() => onSave({calendarId: trimmedCalendarId, calendarName})}/>
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

function describeCalendar(calendar: CalendarListEntry): string {
  const details = [calendar.isDefault ? "default" : "", calendar.canEdit ? "can edit" : "read only", calendar.ownerAddress].filter(Boolean).join(", ");
  return `${calendar.name} (${details})`;
}

function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
