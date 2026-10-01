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
import { isCalendlyEventTypeURI } from "./provider.js";

export interface CalendlyEventType { uri: string; name: string; durationMinutes?: number; isActive: boolean; }

export interface CalendlyUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  eventTypes: CalendlyEventType[];
  isEventTypeListTruncated?: boolean;
  busy?: boolean;
  loadError?: string;
  onLoadEventTypes(): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function CalendlyConfigurationUnit(props: CalendlyUnitProps) {
  switch (props.target.unitId) {
    case "eventTypePicker": return <EventTypePickerUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported Calendly configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function EventTypePickerUnit({target, eventTypes, isEventTypeListTruncated, busy, loadError, onLoadEventTypes, onSave}: CalendlyUnitProps) {
  const [eventTypeURI, setEventTypeURI] = useState(stringValue(target.value.eventTypeUri));
  const isValid = eventTypeURI === "" ? !target.required : isCalendlyEventTypeURI(eventTypeURI);
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadEventTypes}>Load event types</StudioButton>
      <span className="studio-muted">Lists the connected Calendly user's event types.</span>
    </div>
    {isEventTypeListTruncated && <StudioNotice tone="attention">Only the first event types are listed; enter another event type URI below.</StudioNotice>}
    <StudioField label="Event type">
      <select onChange={(event) => setEventTypeURI(event.target.value)} value={eventTypeURI}>
        <option value="">{target.required ? "Select an event type" : "Every event type"}</option>
        {eventTypes.map((eventType) => <option key={eventType.uri} value={eventType.uri}>
          {eventType.name}{eventType.durationMinutes ? ` · ${eventType.durationMinutes} min` : ""}{eventType.isActive ? "" : " (inactive)"}
        </option>)}
      </select>
    </StudioField>
    <StudioField hint="Use the event type URI when it is not listed, such as one owned by another member of the organization." label="Event type URI fallback">
      <input onChange={(event) => setEventTypeURI(event.target.value.trim())} placeholder="https://api.calendly.com/event_types/AAAAAAAAAAAAAAAA" type="text" value={eventTypeURI}/>
    </StudioField>
    {!isValid && eventTypeURI !== "" && <StudioNotice tone="error">Enter a URI that starts with https://api.calendly.com/event_types/.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave({eventTypeUri: eventTypeURI})}/>
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
