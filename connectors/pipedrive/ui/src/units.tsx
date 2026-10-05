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
import {
  isPipedriveCustomFieldKey,
  isPipedriveID,
  pipedriveObjectTypes,
  type PipedriveCustomField,
  type PipedrivePipeline,
  type PipedriveUser,
} from "./provider.js";

export interface PipedriveUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  /** canListFromPipedrive is false for an OAuth connection, whose company API domain Studio commands cannot reach. */
  canListFromPipedrive: boolean;
  owners: PipedriveUser[];
  pipelines: PipedrivePipeline[];
  customFields: PipedriveCustomField[];
  customFieldObjectType: string;
  busy?: boolean;
  loadError?: string;
  onLoadOwners(): void;
  onLoadPipelines(): void;
  onLoadCustomFields(objectType: string): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function PipedriveConfigurationUnit(props: PipedriveUnitProps) {
  switch (props.target.unitId) {
    case "ownerPicker": return <OwnerPickerUnit {...props}/>;
    case "dealStagePicker": return <DealStagePickerUnit {...props}/>;
    case "customFieldPicker": return <CustomFieldPickerUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported Pipedrive configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function OwnerPickerUnit({target, canListFromPipedrive, owners, busy, loadError, onLoadOwners, onSave}: PipedriveUnitProps) {
  const [ownerId, setOwnerId] = useState(stringValue(target.value.ownerId));
  const isValid = isPipedriveID(ownerId);
  return <UnitFrame canListFromPipedrive={canListFromPipedrive} loadError={loadError} target={target}>
    {canListFromPipedrive && <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadOwners}>Load users</StudioButton>
      <span className="studio-muted">Lists active users from Pipedrive Company settings &gt; Manage users.</span>
    </div>}
    <StudioField label="Owner">
      <select onChange={(event) => setOwnerId(event.target.value)} value={ownerId}>
        <option value="">{target.required ? "Select a user" : "No owner"}</option>
        {ownerId !== "" && !owners.some((owner) => owner.id === ownerId) && <option value={ownerId}>Saved user · {ownerId}</option>}
        {owners.map((owner) => <option key={owner.id} value={owner.id}>{owner.name}{owner.email && owner.email !== owner.name ? ` · ${owner.email}` : ""} · {owner.id}</option>)}
      </select>
    </StudioField>
    <StudioField hint="Numeric Pipedrive user ID, such as the owner_id of a record that user owns, for a user the list does not show. Blank sets no owner." label="Owner ID fallback">
      <input inputMode="numeric" onChange={(event) => setOwnerId(event.target.value.trim())} placeholder="1234567" type="text" value={ownerId}/>
    </StudioField>
    {!isValid && <StudioNotice tone="error">A Pipedrive user ID contains only digits.</StudioNotice>}
    <SaveAction disabled={busy || !isValid || (target.required && ownerId === "")} onSave={() => onSave({ownerId})}/>
  </UnitFrame>;
}

function DealStagePickerUnit({target, canListFromPipedrive, pipelines, busy, loadError, onLoadPipelines, onSave}: PipedriveUnitProps) {
  const [pipelineId, setPipelineId] = useState(stringValue(target.value.pipelineId));
  const [stageId, setStageId] = useState(stringValue(target.value.stageId));
  const selectedPipeline = pipelines.find((pipeline) => pipeline.id === pipelineId);
  const isComplete = pipelineId !== "" && stageId !== "";
  const isHalfFilled = (pipelineId === "") !== (stageId === "");
  const isValid = isPipedriveID(pipelineId) && isPipedriveID(stageId);
  return <UnitFrame canListFromPipedrive={canListFromPipedrive} loadError={loadError} target={target}>
    {canListFromPipedrive && <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadPipelines}>Load pipelines</StudioButton>
      <span className="studio-muted">Lists the pipelines and stages shown in the Pipedrive Pipeline view.</span>
    </div>}
    <StudioField label="Pipeline">
      <select onChange={(event) => { setPipelineId(event.target.value); setStageId(""); }} value={pipelineId}>
        <option value="">Select a pipeline</option>
        {pipelineId !== "" && !selectedPipeline && <option value={pipelineId}>Saved pipeline · {pipelineId}</option>}
        {pipelines.map((pipeline) => <option key={pipeline.id} value={pipeline.id}>{pipeline.name} · {pipeline.id}</option>)}
      </select>
    </StudioField>
    <StudioField label="Stage">
      <select disabled={!selectedPipeline && stageId === ""} onChange={(event) => setStageId(event.target.value)} value={stageId}>
        <option value="">Select a stage</option>
        {stageId !== "" && !selectedPipeline?.stages.some((stage) => stage.id === stageId) && <option value={stageId}>Saved stage · {stageId}</option>}
        {selectedPipeline?.stages.map((stage) => <option key={stage.id} value={stage.id}>{stage.name} · {stage.id}</option>)}
      </select>
    </StudioField>
    <StudioField hint="Numeric pipeline ID, shown in the Pipeline view address, such as /pipeline/1, for a pipeline the list does not show." label="Pipeline ID fallback">
      <input inputMode="numeric" onChange={(event) => setPipelineId(event.target.value.trim())} placeholder="1" type="text" value={pipelineId}/>
    </StudioField>
    <StudioField hint="Numeric stage ID in that pipeline, from Pipedrive's stages list or a deal's stage_id." label="Stage ID fallback">
      <input inputMode="numeric" onChange={(event) => setStageId(event.target.value.trim())} placeholder="3" type="text" value={stageId}/>
    </StudioField>
    {isHalfFilled && <StudioNotice tone="error">Choose both a pipeline and a stage.</StudioNotice>}
    {!isValid && <StudioNotice tone="error">Pipeline and stage IDs contain only digits.</StudioNotice>}
    <SaveAction disabled={busy || isHalfFilled || !isValid || (target.required && !isComplete)} onSave={() => onSave({pipelineId, stageId})}/>
  </UnitFrame>;
}

function CustomFieldPickerUnit(props: PipedriveUnitProps) {
  const {target, canListFromPipedrive, customFields, customFieldObjectType, busy, loadError, onLoadCustomFields, onSave} = props;
  const [objectType, setObjectType] = useState(stringValue(target.value.objectType));
  const [fieldKey, setFieldKey] = useState(stringValue(target.value.fieldKey));
  const loadedFields = customFieldObjectType === objectType ? customFields : [];
  const isKnownType = objectType === "" || pipedriveObjectTypes.some((option) => option.id === objectType);
  const isValid = isKnownType && isPipedriveCustomFieldKey(fieldKey) && (fieldKey === "" || objectType !== "");
  return <UnitFrame canListFromPipedrive={canListFromPipedrive} loadError={loadError} target={target}>
    <StudioField hint="Saves Pipedrive's object name: persons, organizations, or deals." label="Object type">
      <select onChange={(event) => { setObjectType(event.target.value); setFieldKey(""); }} value={objectType}>
        <option value="">{target.required ? "Select an object type" : "No custom field"}</option>
        {pipedriveObjectTypes.map((option) => <option key={option.id} value={option.id}>{option.label} · {option.id}</option>)}
      </select>
    </StudioField>
    {canListFromPipedrive && <div className="studio-actions">
      <StudioButton disabled={busy || !isKnownType || objectType === ""} onClick={() => onLoadCustomFields(objectType)}>Load custom fields</StudioButton>
      <span className="studio-muted">Lists the custom fields from Pipedrive Company settings &gt; Data fields.</span>
    </div>}
    <StudioField label="Custom field">
      <select onChange={(event) => setFieldKey(event.target.value)} value={fieldKey}>
        <option value="">No custom field</option>
        {fieldKey !== "" && !loadedFields.some((field) => field.key === fieldKey) && <option value={fieldKey}>Saved field · {fieldKey}</option>}
        {loadedFields.map((field) => <option key={field.key} value={field.key}>{field.name} ({field.fieldType}{field.isText ? "" : ", not text"}) · {field.key}</option>)}
      </select>
    </StudioField>
    <StudioField hint="The field's 40-character API key, which Pipedrive shows for each custom field under Company settings > Data fields. Blank writes no custom field." label="Field key fallback">
      <input onChange={(event) => setFieldKey(event.target.value.trim())} placeholder="40 lowercase hexadecimal characters" type="text" value={fieldKey}/>
    </StudioField>
    {!isKnownType && <StudioNotice tone="error">The saved object type is not supported by this connector.</StudioNotice>}
    {isKnownType && !isValid && <StudioNotice tone="error">A custom field key is 40 lowercase hexadecimal characters and needs an object type.</StudioNotice>}
    <SaveAction disabled={busy || !isValid || (target.required && fieldKey === "")} onSave={() => onSave({objectType: fieldKey === "" ? "" : objectType, fieldKey})}/>
  </UnitFrame>;
}

function UnitFrame({target, canListFromPipedrive, loadError, children}: {target: ConnectorStudioConfigurationUnitTarget; canListFromPipedrive: boolean; loadError?: string; children: ReactNode}) {
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {!canListFromPipedrive && <StudioNotice tone="info">Lists load only for a Personal API token connection; enter the ID below.</StudioNotice>}
    {loadError && <StudioNotice tone="error">{loadError}. Enter the ID below instead.</StudioNotice>}
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
