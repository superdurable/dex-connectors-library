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
import { isTypeformFormID, type TypeformForm } from "./provider.js";

export interface TypeformUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  forms: TypeformForm[];
  isFormListTruncated?: boolean;
  busy?: boolean;
  loadError?: string;
  onLoadForms(): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function TypeformConfigurationUnit(props: TypeformUnitProps) {
  switch (props.target.unitId) {
    case "formPicker": return <FormPickerUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported Typeform configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function FormPickerUnit({target, forms, isFormListTruncated, busy, loadError, onLoadForms, onSave}: TypeformUnitProps) {
  const [formID, setFormID] = useState(stringValue(target.value.formId));
  const isValid = formID === "" ? !target.required : isTypeformFormID(formID);
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadForms}>Load forms</StudioButton>
      <span className="studio-muted">Lists the connected Typeform account's forms.</span>
    </div>
    {isFormListTruncated && <StudioNotice tone="attention">Only the first 4,000 forms are listed; enter another form ID below.</StudioNotice>}
    <StudioField label="Form">
      <select onChange={(event) => setFormID(event.target.value)} value={formID}>
        <option value="">{target.required ? "Select a form" : "Every form"}</option>
        {forms.map((form) => <option key={form.id} value={form.id}>
          {form.title} · {form.id}{form.isPublic ? "" : " (closed)"}
        </option>)}
      </select>
    </StudioField>
    <StudioField hint="Use the form ID when the form is not listed: the part after /to/ in its link, such as u6nXL7 in https://form.typeform.com/to/u6nXL7." label="Form ID fallback">
      <input onChange={(event) => setFormID(event.target.value.trim())} placeholder="u6nXL7" type="text" value={formID}/>
    </StudioField>
    {!isValid && formID !== "" && <StudioNotice tone="error">Enter a form ID of letters, digits, '_', or '-'.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave({formId: formID})}/>
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
