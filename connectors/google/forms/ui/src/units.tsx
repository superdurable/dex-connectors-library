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
import { formIDFromInput, isResponderLink, type GoogleForm } from "./provider.js";

export interface FormsUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  forms?: GoogleForm[];
  /** isFormListTruncated reports that paging stopped before Drive's last page. */
  isFormListTruncated?: boolean;
  busy?: boolean;
  loadError?: string;
  onChooseForm(): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function FormsConfigurationUnit(props: FormsUnitProps) {
  if (props.target.unitId === "formPicker") return <FormPickerUnit {...props}/>;
  const {target} = props;
  return <StudioNotice tone="error">Unsupported Google Forms configuration unit: {target.unitId}</StudioNotice>;
}

function FormPickerUnit({target, busy, forms = [], isFormListTruncated, loadError, onChooseForm, onSave}: FormsUnitProps) {
  const [formInput, setFormInput] = useState(stringValue(target.value.formId));
  const [formTitle, setFormTitle] = useState(stringValue(target.value.formTitle));
  const formId = formIDFromInput(formInput);
  const isFormInputInvalid = formInput.trim() !== "" && formId === "";
  const chooseListedForm = (id: string) => {
    setFormInput(id);
    setFormTitle(forms.find((form) => form.id === id)?.title ?? "");
  };
  const editFormInput = (value: string) => {
    setFormInput(value);
    setFormTitle(forms.find((form) => form.id === formIDFromInput(value))?.title ?? "");
  };
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onChooseForm}>Choose form</StudioButton>
      {formTitle && <span className="studio-muted"><strong>Form:</strong> {formTitle}</span>}
    </div>
    {forms.length > 0 && <StudioField label="Form">
      <select onChange={(event) => chooseListedForm(event.target.value)} value={formId}>
        <option value="">{target.required ? "Select a form" : "No form"}</option>
        {forms.map((form) => <option key={form.id} value={form.id}>{form.title}</option>)}
      </select>
    </StudioField>}
    {isFormListTruncated && <StudioNotice tone="attention">
      Google Drive returned more forms than one list can show. Enter a form ID to use one that is not listed.
    </StudioNotice>}
    <StudioField hint="Paste a form ID or its docs.google.com/forms/d/FORM_ID/edit link when the form is not listed. Leave blank for no form." label="Form ID">
      <input onChange={(event) => editFormInput(event.target.value)} type="text" value={formInput}/>
    </StudioField>
    {isFormInputInvalid && <StudioNotice tone="error">{isResponderLink(formInput)
      ? "That is the link respondents open, which carries a different ID. Open the form in Google Forms and paste its edit link instead."
      : "Enter a Google Forms form ID or a docs.google.com/forms/d/FORM_ID/edit link."}</StudioNotice>}
    <SaveAction
      disabled={busy || isFormInputInvalid || (target.required && formId === "")}
      onSave={() => onSave({formId, formTitle: formId === "" ? "" : formTitle})}
    />
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
