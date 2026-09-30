// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StudioButton,
  StudioField,
  StudioHeader,
  StudioNotice,
  StudioSurface,
  type ConnectorStudioConfigurationUnitTarget,
  type ConnectorStudioConnectionTarget,
} from "@superdurable/dex-connectors-react";
import { useState } from "react";
import { isCloudID, isProjectKey, type JiraProject, type JiraSite } from "./provider.js";

const tenantInfoHint = "Sign in to Jira, open https://<your-site>.atlassian.net/_edge/tenant_info in the same browser, and copy its cloudId: a UUID such as 1324a887-45db-1bf4-1e99-ef0ff456d421.";

export interface SitePickerUnitProps {
  target: ConnectorStudioConnectionTarget;
  sites?: JiraSite[];
  busy?: boolean;
  loadError?: string;
  onChooseSite(): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

/** SitePickerUnit renders the connection's cloudId field when the host shows the sitePicker unit inline. */
export function SitePickerUnit({target, sites = [], busy, loadError, onChooseSite, onSave}: SitePickerUnitProps) {
  const [cloudId, setCloudId] = useState(stringValue(target.value?.cloudId));
  const trimmedCloudId = cloudId.trim();
  const isValid = trimmedCloudId === "" || isCloudID(trimmedCloudId);
  return <StudioSurface label="Jira site">
    <StudioHeader description="Choose the Jira Cloud site every operation of this connection uses. Leave it blank to use the only Jira site the authorization grants." title="Jira site"/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <div className="studio-actions"><StudioButton disabled={busy} onClick={onChooseSite}>Choose Jira site</StudioButton></div>
    {sites.length > 0 && <StudioField label="Site">
      <select onChange={(event) => setCloudId(event.target.value)} value={cloudId}>
        <option value="">Select a Jira site</option>
        {sites.map((site) => <option key={site.cloudId} value={site.cloudId}>{describeSite(site)}</option>)}
      </select>
    </StudioField>}
    <StudioField hint={tenantInfoHint} label="Site cloudId">
      <input onChange={(event) => setCloudId(event.target.value)} placeholder="1324a887-45db-1bf4-1e99-ef0ff456d421" type="text" value={cloudId}/>
    </StudioField>
    {!isValid && <StudioNotice tone="error">The cloudId must be a UUID; a site URL is not accepted.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave({cloudId: trimmedCloudId.toLowerCase()})}/>
  </StudioSurface>;
}

export interface ProjectPickerUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  /** connectionCloudId is the connection's saved site, or "" when the host does not report it. */
  connectionCloudId: string;
  projects?: JiraProject[];
  isProjectListTruncated?: boolean;
  busy?: boolean;
  loadError?: string;
  onChooseProject(cloudId: string): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function JiraConfigurationUnit(props: ProjectPickerUnitProps) {
  if (props.target.unitId === "projectPicker") return <ProjectPickerUnit {...props}/>;
  return <StudioNotice tone="error">Unsupported Jira configuration unit: {props.target.unitId}</StudioNotice>;
}

function ProjectPickerUnit({target, connectionCloudId, projects = [], isProjectListTruncated, busy, loadError, onChooseProject, onSave}: ProjectPickerUnitProps) {
  const [listingCloudId, setListingCloudId] = useState(connectionCloudId);
  const [projectKey, setProjectKey] = useState(stringValue(target.value.projectKey));
  const [project, setProject] = useState<JiraProject | undefined>(savedProject(target));
  const trimmedProjectKey = projectKey.trim();
  const isProjectKeyValid = trimmedProjectKey === "" || isProjectKey(trimmedProjectKey);
  const canList = isCloudID(listingCloudId);
  const choose = (key: string) => {
    setProjectKey(key);
    setProject(projects.find((candidate) => candidate.key === key));
  };
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    {connectionCloudId === "" && <StudioField hint={"Used only to list projects; the connection's own cloudId still selects the site. " + tenantInfoHint} label="Jira site cloudId">
      <input onChange={(event) => setListingCloudId(event.target.value.trim())} placeholder="1324a887-45db-1bf4-1e99-ef0ff456d421" type="text" value={listingCloudId}/>
    </StudioField>}
    <div className="studio-actions">
      <StudioButton disabled={busy || !canList} onClick={() => onChooseProject(listingCloudId.toLowerCase())}>Choose project</StudioButton>
      {project && <span className="studio-muted"><strong>Project:</strong> {project.name} ({project.key})</span>}
    </div>
    {projects.length > 0 && <StudioField label="Project">
      <select onChange={(event) => choose(event.target.value)} value={projectKey}>
        <option value="">Select a project</option>
        {projects.map((candidate) => <option key={candidate.id} value={candidate.key}>{candidate.name} ({candidate.key})</option>)}
      </select>
    </StudioField>}
    {isProjectListTruncated && <StudioNotice tone="attention">
      Jira returned more projects than one list can show. Enter the project key to use one that is not listed.
    </StudioNotice>}
    <StudioField hint="Uppercase project key, shown in Jira > Projects > View all projects under Key, such as OPS. Leave it blank to use each Start Flow input's projectKey." label="Project key">
      <input onChange={(event) => { setProjectKey(event.target.value.toUpperCase()); setProject(undefined); }} placeholder="OPS" type="text" value={projectKey}/>
    </StudioField>
    {!isProjectKeyValid && <StudioNotice tone="error">The project key must be uppercase letters, digits, or underscores, starting with a letter.</StudioNotice>}
    <SaveAction
      disabled={busy || !isProjectKeyValid || (target.required && trimmedProjectKey === "")}
      onSave={() => onSave({projectId: project?.id ?? "", projectKey: trimmedProjectKey, projectName: project?.name ?? ""})}
    />
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

function describeSite(site: JiraSite): string {
  return site.url ? `${site.name} (${site.url})` : site.name;
}

function savedProject(target: ConnectorStudioConfigurationUnitTarget): JiraProject | undefined {
  const key = stringValue(target.value.projectKey);
  if (key === "") return undefined;
  return {id: stringValue(target.value.projectId), key, name: stringValue(target.value.projectName) || key};
}

function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
