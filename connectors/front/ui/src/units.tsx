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
import { frontResourcePickers, isFrontResourceID, type FrontResource, type FrontResourcePicker } from "./resources.js";

/** FrontUnitProps carries one configuration unit target, its listed resources, and the load and save callbacks. */
export interface FrontUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  resources: FrontResource[];
  isListTruncated?: boolean;
  busy?: boolean;
  loadError?: string;
  onLoadResources(picker: FrontResourcePicker): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

/** pickerForUnit returns the picker a unit ID names, or undefined for a unit this bundle does not know. */
export function pickerForUnit(unitId: string): FrontResourcePicker | undefined {
  return Object.values(frontResourcePickers).find((picker) => picker.unitId === unitId);
}

/** FrontConfigurationUnit renders the picker a unit ID names, or an error notice for an unknown unit. */
export function FrontConfigurationUnit(props: FrontUnitProps) {
  const picker = pickerForUnit(props.target.unitId);
  if (picker === undefined) return <StudioNotice tone="error">Unsupported Front configuration unit: {props.target.unitId}</StudioNotice>;
  return <ResourcePickerUnit {...props} picker={picker}/>;
}

function ResourcePickerUnit({target, picker, resources, isListTruncated, busy, loadError, onLoadResources, onSave}: FrontUnitProps & {picker: FrontResourcePicker}) {
  const [resourceID, setResourceID] = useState(stringValue(target.value[picker.port]));
  const isValid = resourceID === "" ? !target.required : isFrontResourceID(picker, resourceID);
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={() => onLoadResources(picker)}>Load {picker.pluralNoun}</StudioButton>
      <span className="studio-muted">Lists the {picker.pluralNoun} the connection's Front API token can see.</span>
    </div>
    {isListTruncated && <StudioNotice tone="attention">Only the first {picker.pluralNoun} are listed; enter another {picker.noun} ID below.</StudioNotice>}
    <StudioField label={capitalize(picker.noun)}>
      <select onChange={(event) => setResourceID(event.target.value)} value={resourceID}>
        <option value="">{target.required ? `Select ${picker.indefiniteNoun}` : `No ${picker.noun}`}</option>
        {resources.map((resource) => <option key={resource.id} value={resource.id}>
          {resource.label}{resource.detail ? ` · ${resource.detail}` : ""} · {resource.id}
        </option>)}
      </select>
    </StudioField>
    <StudioField hint={picker.fallbackHint} label={`${capitalize(picker.noun)} ID fallback`}>
      <input onChange={(event) => setResourceID(event.target.value.trim())} placeholder={picker.idExample} type="text" value={resourceID}/>
    </StudioField>
    {!isValid && resourceID !== "" && <StudioNotice tone="error">A Front {picker.noun} ID looks like {picker.idExample}.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave(resourceID === "" ? {} : {[picker.port]: resourceID})}/>
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
function capitalize(value: string): string { return value.charAt(0).toUpperCase() + value.slice(1); }
