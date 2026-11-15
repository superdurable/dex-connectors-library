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
import { changeTypes, savedChangeType } from "./change-types.js";

export interface BambooHRUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  busy?: boolean;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function BambooHRConfigurationUnit(props: BambooHRUnitProps) {
  switch (props.target.unitId) {
    case "changeTypePicker": return <ChangeTypePickerUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported BambooHR configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function ChangeTypePickerUnit({target, busy, onSave}: BambooHRUnitProps) {
  const [selected, setSelected] = useState(savedChangeType(target.value.changeType));
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    <fieldset aria-label="Employee changes" className="studio-options">
      {changeTypes.map(({changeType, label, description}) => <label className="studio-option" key={changeType || "every"}>
        <input checked={selected === changeType} name="changeType" onChange={() => setSelected(changeType)} type="radio"/>
        <span className="studio-option-label">{label}</span>
        <span className="studio-option-id">{description}</span>
      </label>)}
    </fieldset>
    <p className="studio-muted">BambooHR reports only each employee&apos;s latest change, so with Every change an employee added and then edited before the next poll arrives as Updated. The first poll after you save starts at the current time; earlier changes never start Flows. A change starts a Flow once it is 30 seconds old, at the next poll after that.</p>
    <SaveAction disabled={busy} onSave={() => onSave({changeType: selected})}/>
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
