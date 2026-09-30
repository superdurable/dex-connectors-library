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
import { isHubSpotOwnerID, type HubSpotDealPipeline, type HubSpotOwner } from "./provider.js";

export const hubspotObjectTypes = [
  {id: "contacts", label: "Contacts"},
  {id: "companies", label: "Companies"},
  {id: "deals", label: "Deals"},
] as const;

export interface HubSpotUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  owners: HubSpotOwner[];
  isOwnerListTruncated?: boolean;
  pipelines: HubSpotDealPipeline[];
  busy?: boolean;
  loadError?: string;
  onLoadOwners(): void;
  onLoadPipelines(): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function HubSpotConfigurationUnit(props: HubSpotUnitProps) {
  switch (props.target.unitId) {
    case "objectTypePicker": return <ObjectTypePickerUnit {...props}/>;
    case "ownerPicker": return <OwnerPickerUnit {...props}/>;
    case "dealStagePicker": return <DealStagePickerUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported HubSpot configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function ObjectTypePickerUnit({target, busy, onSave}: HubSpotUnitProps) {
  const [objectType, setObjectType] = useState(stringValue(target.value.objectType));
  const isKnown = objectType === "" || hubspotObjectTypes.some((option) => option.id === objectType);
  return <UnitFrame target={target}>
    <StudioField hint="Saves HubSpot's object name: contacts, companies, or deals." label="Object type">
      <select onChange={(event) => setObjectType(event.target.value)} value={objectType}>
        <option value="">{target.required ? "Select an object type" : "No object type"}</option>
        {hubspotObjectTypes.map((option) => <option key={option.id} value={option.id}>{option.label} · {option.id}</option>)}
      </select>
    </StudioField>
    {!isKnown && <StudioNotice tone="error">The saved object type is not supported by this connector.</StudioNotice>}
    <SaveAction disabled={busy || !isKnown || (target.required && objectType === "")} onSave={() => onSave({objectType})}/>
  </UnitFrame>;
}

function OwnerPickerUnit({target, owners, isOwnerListTruncated, busy, loadError, onLoadOwners, onSave}: HubSpotUnitProps) {
  const [ownerId, setOwnerId] = useState(stringValue(target.value.ownerId));
  const isValid = isHubSpotOwnerID(ownerId);
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadOwners}>Load owners</StudioButton>
      <span className="studio-muted">Lists active owners from HubSpot Settings &gt; Users &amp; Teams.</span>
    </div>
    {isOwnerListTruncated && <StudioNotice tone="attention">HubSpot has more owners than this list shows; enter an owner ID below if yours is missing.</StudioNotice>}
    <StudioField label="Owner">
      <select onChange={(event) => setOwnerId(event.target.value)} value={ownerId}>
        <option value="">{target.required ? "Select an owner" : "No owner (leave unchanged)"}</option>
        {ownerId !== "" && !owners.some((owner) => owner.id === ownerId) && <option value={ownerId}>Saved owner · {ownerId}</option>}
        {owners.map((owner) => <option key={owner.id} value={owner.id}>{owner.displayName}{owner.email && owner.email !== owner.displayName ? ` · ${owner.email}` : ""}{owner.isQueue ? " (queue)" : ""} · {owner.id}</option>)}
      </select>
    </StudioField>
    <StudioField hint="Numeric owner ID from HubSpot Settings > Users & Teams, for an owner the list does not show. Blank means no owner is set." label="Owner ID fallback">
      <input inputMode="numeric" onChange={(event) => setOwnerId(event.target.value.trim())} placeholder="123456789" type="text" value={ownerId}/>
    </StudioField>
    {!isValid && <StudioNotice tone="error">An owner ID contains only digits.</StudioNotice>}
    <SaveAction disabled={busy || !isValid || (target.required && ownerId === "")} onSave={() => onSave({ownerId})}/>
  </UnitFrame>;
}

function DealStagePickerUnit({target, pipelines, busy, loadError, onLoadPipelines, onSave}: HubSpotUnitProps) {
  const [pipelineId, setPipelineId] = useState(stringValue(target.value.pipelineId));
  const [stageId, setStageId] = useState(stringValue(target.value.stageId));
  const selectedPipeline = pipelines.find((pipeline) => pipeline.id === pipelineId);
  const isComplete = pipelineId !== "" && stageId !== "";
  const isHalfFilled = (pipelineId === "") !== (stageId === "");
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadPipelines}>Load deal pipelines</StudioButton>
      <span className="studio-muted">Lists the deal pipelines and stages from HubSpot Settings &gt; Objects &gt; Deals &gt; Pipelines.</span>
    </div>
    <StudioField label="Pipeline">
      <select onChange={(event) => { setPipelineId(event.target.value); setStageId(""); }} value={pipelineId}>
        <option value="">Select a pipeline</option>
        {pipelineId !== "" && !selectedPipeline && <option value={pipelineId}>Saved pipeline · {pipelineId}</option>}
        {pipelines.map((pipeline) => <option key={pipeline.id} value={pipeline.id}>{pipeline.label} · {pipeline.id}</option>)}
      </select>
    </StudioField>
    <StudioField label="Stage">
      <select disabled={!selectedPipeline && stageId === ""} onChange={(event) => setStageId(event.target.value)} value={stageId}>
        <option value="">Select a stage</option>
        {stageId !== "" && !selectedPipeline?.stages.some((stage) => stage.id === stageId) && <option value={stageId}>Saved stage · {stageId}</option>}
        {selectedPipeline?.stages.map((stage) => <option key={stage.id} value={stage.id}>{stage.label}{stage.isClosed ? " (closed)" : ""} · {stage.id}</option>)}
      </select>
    </StudioField>
    <StudioField hint="Internal pipeline ID, such as default, for a pipeline the list does not show." label="Pipeline ID fallback">
      <input onChange={(event) => setPipelineId(event.target.value.trim())} placeholder="default" type="text" value={pipelineId}/>
    </StudioField>
    <StudioField hint="Internal stage ID, such as qualifiedtobuy, in that pipeline." label="Stage ID fallback">
      <input onChange={(event) => setStageId(event.target.value.trim())} placeholder="qualifiedtobuy" type="text" value={stageId}/>
    </StudioField>
    {isHalfFilled && <StudioNotice tone="error">Choose both a pipeline and a stage.</StudioNotice>}
    <SaveAction disabled={busy || isHalfFilled || (target.required && !isComplete)} onSave={() => onSave({pipelineId, stageId})}/>
  </UnitFrame>;
}

function UnitFrame({target, loadError, children}: {target: ConnectorStudioConfigurationUnitTarget; loadError?: string; children: ReactNode}) {
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
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
