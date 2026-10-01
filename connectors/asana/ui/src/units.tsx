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
import { isAsanaGID, type AsanaResource } from "./provider.js";

const gidHint = "The gid is the long number in the item's Asana web address, such as 1204567890123456.";

/** AsanaResourceList is one loaded list and whether Asana had more entries than the picker reads. */
export interface AsanaResourceList {
  resources: AsanaResource[];
  isTruncated: boolean;
}

export interface AsanaConfigurationUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  workspaces?: AsanaResourceList;
  projects?: AsanaResourceList;
  sections?: AsanaResourceList;
  busy?: boolean;
  loadError?: string;
  onLoadWorkspaces(): void;
  onLoadProjects(workspaceId: string): void;
  onLoadSections(projectId: string): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

/** AsanaConfigurationUnit renders the workspace or project picker a Flow Step composes. */
export function AsanaConfigurationUnit(props: AsanaConfigurationUnitProps) {
  if (props.target.unitId === "workspacePicker") return <WorkspacePickerUnit {...props}/>;
  if (props.target.unitId === "projectPicker") return <ProjectPickerUnit {...props}/>;
  return <StudioNotice tone="error">Unsupported Asana configuration unit: {props.target.unitId}</StudioNotice>;
}

function WorkspacePickerUnit({target, workspaces, busy, loadError, onLoadWorkspaces, onSave}: AsanaConfigurationUnitProps) {
  const [workspaceId, setWorkspaceId] = useState(stringValue(target.value.workspaceId));
  const trimmedWorkspaceId = workspaceId.trim();
  const isValid = trimmedWorkspaceId === "" || isAsanaGID(trimmedWorkspaceId);
  const workspaceName = nameFor(workspaces, trimmedWorkspaceId) || (trimmedWorkspaceId === stringValue(target.value.workspaceId) ? stringValue(target.value.workspaceName) : "");
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <div className="studio-actions"><StudioButton disabled={busy} onClick={onLoadWorkspaces}>Choose workspace</StudioButton></div>
    <ResourceSelect label="Workspace" list={workspaces} onChoose={setWorkspaceId} placeholder="Select a workspace" value={trimmedWorkspaceId}/>
    <StudioField hint={"Workspace gid, from the list above or from the workspace's Asana web address. " + gidHint + " Leave it blank only when the description above says the Step allows no workspace."} label="Workspace gid">
      <input onChange={(event) => setWorkspaceId(event.target.value)} placeholder="1100000000000001" type="text" value={workspaceId}/>
    </StudioField>
    {!isValid && <StudioNotice tone="error">The workspace gid must be digits only; a workspace name is not accepted.</StudioNotice>}
    <SaveAction
      disabled={busy || !isValid || (target.required && trimmedWorkspaceId === "")}
      onSave={() => onSave({workspaceId: trimmedWorkspaceId, workspaceName})}
    />
  </StudioSurface>;
}

function ProjectPickerUnit({target, workspaces, projects, sections, busy, loadError, onLoadWorkspaces, onLoadProjects, onLoadSections, onSave}: AsanaConfigurationUnitProps) {
  const [workspaceId, setWorkspaceId] = useState(stringValue(target.value.workspaceId));
  const [projectId, setProjectId] = useState(stringValue(target.value.projectId));
  const [sectionId, setSectionId] = useState(stringValue(target.value.sectionId));
  const trimmedWorkspaceId = workspaceId.trim(), trimmedProjectId = projectId.trim(), trimmedSectionId = sectionId.trim();
  const isProjectValid = trimmedProjectId === "" || isAsanaGID(trimmedProjectId);
  const isSectionValid = trimmedSectionId === "" || isAsanaGID(trimmedSectionId);
  const projectName = nameFor(projects, trimmedProjectId) || (trimmedProjectId === stringValue(target.value.projectId) ? stringValue(target.value.projectName) : "");
  const sectionName = nameFor(sections, trimmedSectionId) || (trimmedSectionId === stringValue(target.value.sectionId) ? stringValue(target.value.sectionName) : "");
  const chooseProject = (id: string) => { setProjectId(id); setSectionId(""); };
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadWorkspaces}>Choose workspace</StudioButton>
      <StudioButton disabled={busy || !isAsanaGID(trimmedWorkspaceId)} onClick={() => onLoadProjects(trimmedWorkspaceId)}>Choose project</StudioButton>
      <StudioButton disabled={busy || trimmedProjectId === "" || !isProjectValid} onClick={() => onLoadSections(trimmedProjectId)}>Choose section</StudioButton>
    </div>
    <ResourceSelect label="Workspace" list={workspaces} onChoose={setWorkspaceId} placeholder="Select a workspace" value={trimmedWorkspaceId}/>
    <ResourceSelect label="Project" list={projects} onChoose={chooseProject} placeholder="Select a project" value={trimmedProjectId}/>
    <ResourceSelect label="Section" list={sections} onChoose={setSectionId} placeholder="No section: keep Asana's default" value={trimmedSectionId}/>
    {projectName && <p className="studio-muted"><strong>Project:</strong> {projectName}{sectionName ? ` › ${sectionName}` : ""}</p>}
    <StudioField hint={"Project gid, from the list above or from the project's Asana web address. " + gidHint + " Leave it blank to use each Start Flow input's projectId."} label="Project gid">
      <input onChange={(event) => chooseProject(event.target.value)} placeholder="1201000000000001" type="text" value={projectId}/>
    </StudioField>
    {!isProjectValid && <StudioNotice tone="error">The project gid must be digits only; a project name is not accepted.</StudioNotice>}
    <StudioField hint={"Optional section gid in that project, from the list above. Leave it blank to keep Asana's default section for new tasks and leave a reused task's sections unchanged."} label="Section gid">
      <input onChange={(event) => setSectionId(event.target.value)} placeholder="1201000000000101" type="text" value={sectionId}/>
    </StudioField>
    {!isSectionValid && <StudioNotice tone="error">The section gid must be digits only.</StudioNotice>}
    <SaveAction
      disabled={busy || !isProjectValid || !isSectionValid || (trimmedSectionId !== "" && trimmedProjectId === "") || (target.required && trimmedProjectId === "")}
      onSave={() => onSave({workspaceId: trimmedWorkspaceId, projectId: trimmedProjectId, projectName, sectionId: trimmedSectionId, sectionName})}
    />
  </StudioSurface>;
}

interface ResourceSelectProps {
  label: string;
  list?: AsanaResourceList;
  placeholder: string;
  value: string;
  onChoose(id: string): void;
}

function ResourceSelect({label, list, placeholder, value, onChoose}: ResourceSelectProps) {
  if (!list) return null;
  return <>
    <StudioField label={label}>
      <select onChange={(event) => onChoose(event.target.value)} value={value}>
        <option value="">{placeholder}</option>
        {list.resources.map((resource) => <option key={resource.id} value={resource.id}>{resource.name}</option>)}
      </select>
    </StudioField>
    {list.resources.length === 0 && <StudioNotice tone="info">Asana returned no {label.toLowerCase()} for this choice. Enter its gid below instead.</StudioNotice>}
    {list.isTruncated && <StudioNotice tone="attention">Asana returned more entries than one list can show. Enter the gid below to use one that is not listed.</StudioNotice>}
  </>;
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

function nameFor(list: AsanaResourceList | undefined, id: string): string {
  return list?.resources.find((resource) => resource.id === id)?.name ?? "";
}

function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
