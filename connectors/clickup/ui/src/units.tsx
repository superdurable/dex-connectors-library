// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StudioButton,
  StudioHeader,
  StudioNotice,
  StudioSurface,
  type ConnectorStudioConfigurationUnitTarget,
} from "@superdurable/dex-connectors-react";
import { useState } from "react";
import { selectedSupportedEvents, taskEvents } from "./events.js";

export interface ClickUpUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  busy?: boolean;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function ClickUpConfigurationUnit(props: ClickUpUnitProps) {
  switch (props.target.unitId) {
    case "taskEventPicker": return <TaskEventPickerUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported ClickUp configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function TaskEventPickerUnit({target, busy, onSave}: ClickUpUnitProps) {
  const [events, setEvents] = useState(selectedSupportedEvents(target.value.events));
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    <fieldset aria-label="Task events" className="studio-options">
      {taskEvents.map(({event, description}) => <label className="studio-option" key={event}>
        <input checked={events.includes(event)} onChange={(change) => setEvents(change.target.checked
          ? selectedSupportedEvents([...events, event]) : events.filter((selected) => selected !== event))} type="checkbox"/>
        <span className="studio-option-label">{description}</span>
        <span className="studio-option-id">{event}</span>
      </label>)}
    </fieldset>
    <p className="studio-muted">Select none to accept every task event listed. ClickUp sends only the events its webhook subscribes to, which you choose when you create the webhook with POST /api/v2/team/&#123;team_id&#125;/webhook.</p>
    <SaveAction disabled={busy || (target.required && events.length === 0)} onSave={() => onSave({events})}/>
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
