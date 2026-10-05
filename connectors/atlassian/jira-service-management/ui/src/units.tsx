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
import { isCloudID, isNumericID, isProjectKey, type RequestType, type ServiceDesk, type ServiceManagementSite } from "./provider.js";

const tenantInfoHint = "Sign in to Jira, open https://<your-site>.atlassian.net/_edge/tenant_info in the same browser, and copy its cloudId: a UUID such as 1324a887-45db-1bf4-1e99-ef0ff456d421.";
const serviceDeskIDHint = "Numeric service desk ID, such as 10, which Choose service desk fills in. To find it by hand, sign in to Jira and open https://<your-site>.atlassian.net/rest/servicedeskapi/servicedesk in the same browser; each desk's id is listed beside its projectKey.";

export interface SitePickerUnitProps {
  target: ConnectorStudioConnectionTarget;
  sites?: ServiceManagementSite[];
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
  return <StudioSurface label="Jira Service Management site">
    <StudioHeader description="Choose the Atlassian site with Jira Service Management that every operation of this connection uses. Leave it blank to use the only such site the authorization grants." title="Site"/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <div className="studio-actions"><StudioButton disabled={busy} onClick={onChooseSite}>Choose site</StudioButton></div>
    {sites.length > 0 && <StudioField label="Site">
      <select onChange={(event) => setCloudId(event.target.value)} value={cloudId}>
        <option value="">Select a site</option>
        {sites.map((site) => <option key={site.cloudId} value={site.cloudId}>{site.url ? `${site.name} (${site.url})` : site.name}</option>)}
      </select>
    </StudioField>}
    <StudioField hint={tenantInfoHint} label="Site cloudId">
      <input onChange={(event) => setCloudId(event.target.value)} placeholder="1324a887-45db-1bf4-1e99-ef0ff456d421" type="text" value={cloudId}/>
    </StudioField>
    {!isValid && <StudioNotice tone="error">The cloudId must be a UUID; a site URL is not accepted.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave({cloudId: trimmedCloudId.toLowerCase()})}/>
  </StudioSurface>;
}

export interface ServiceManagementUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  /** connectionCloudId is the connection's saved site, or "" when the host does not report it. */
  connectionCloudId: string;
  serviceDesks?: ServiceDesk[];
  requestTypes?: RequestType[];
  isListTruncated?: boolean;
  busy?: boolean;
  loadError?: string;
  onChooseServiceDesk(cloudId: string): void;
  onChooseRequestType(cloudId: string, serviceDeskId: string): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function ServiceManagementConfigurationUnit(props: ServiceManagementUnitProps) {
  if (props.target.unitId === "serviceDeskPicker") return <ServiceDeskPickerUnit {...props}/>;
  if (props.target.unitId === "requestTypePicker") return <RequestTypePickerUnit {...props}/>;
  return <StudioNotice tone="error">Unsupported Jira Service Management configuration unit: {props.target.unitId}</StudioNotice>;
}

function ServiceDeskPickerUnit({target, connectionCloudId, serviceDesks = [], isListTruncated, busy, loadError, onChooseServiceDesk, onSave}: ServiceManagementUnitProps) {
  const [listingCloudId, setListingCloudId] = useState(connectionCloudId);
  const [serviceDeskId, setServiceDeskId] = useState(stringValue(target.value.serviceDeskId));
  const [projectKey, setProjectKey] = useState(stringValue(target.value.projectKey));
  const [serviceDeskName, setServiceDeskName] = useState(stringValue(target.value.serviceDeskName));
  const choose = (id: string) => {
    const desk = serviceDesks.find((candidate) => candidate.id === id);
    setServiceDeskId(id);
    setProjectKey(desk?.projectKey ?? "");
    setServiceDeskName(desk?.name ?? "");
  };
  const isComplete = serviceDeskId.trim() === "" && projectKey.trim() === "" || isNumericID(serviceDeskId) && isProjectKey(projectKey);
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <ListingSiteField connectionCloudId={connectionCloudId} listingCloudId={listingCloudId} onChange={setListingCloudId}/>
    <div className="studio-actions">
      <StudioButton disabled={busy || !isCloudID(listingCloudId)} onClick={() => onChooseServiceDesk(listingCloudId.toLowerCase())}>Choose service desk</StudioButton>
      {serviceDeskName && <span className="studio-muted"><strong>Service desk:</strong> {serviceDeskName} ({projectKey})</span>}
    </div>
    {serviceDesks.length > 0 && <StudioField label="Service desk">
      <select onChange={(event) => choose(event.target.value)} value={serviceDeskId}>
        <option value="">Select a service desk</option>
        {serviceDesks.map((desk) => <option key={desk.id} value={desk.id}>{desk.name} ({desk.projectKey})</option>)}
      </select>
    </StudioField>}
    {isListTruncated && <StudioNotice tone="attention">Jira Service Management returned more service desks than one list can show. Enter the desk below to use one that is not listed.</StudioNotice>}
    <StudioField hint={serviceDeskIDHint} label="Service desk ID">
      <input onChange={(event) => { setServiceDeskId(event.target.value.trim()); setServiceDeskName(""); }} placeholder="10" type="text" value={serviceDeskId}/>
    </StudioField>
    <StudioField hint="Uppercase key of the service desk's Jira project, shown in Jira > Projects > View all projects under Key, such as ITH; ticket search uses it. Leave both fields blank to use each Start Flow input's serviceDeskId and projectKey." label="Project key">
      <input onChange={(event) => { setProjectKey(event.target.value.toUpperCase()); setServiceDeskName(""); }} placeholder="ITH" type="text" value={projectKey}/>
    </StudioField>
    {!isComplete && <StudioNotice tone="error">Enter both a numeric service desk ID and an uppercase project key, or leave both blank.</StudioNotice>}
    <SaveAction
      disabled={busy || !isComplete || (target.required && serviceDeskId.trim() === "")}
      onSave={() => onSave({serviceDeskId: serviceDeskId.trim(), projectKey: projectKey.trim(), serviceDeskName})}
    />
  </StudioSurface>;
}

function RequestTypePickerUnit(props: ServiceManagementUnitProps) {
  const {target, connectionCloudId, serviceDesks = [], requestTypes = [], isListTruncated, busy, loadError, onChooseServiceDesk, onChooseRequestType, onSave} = props;
  const [listingCloudId, setListingCloudId] = useState(connectionCloudId);
  const [serviceDeskId, setServiceDeskId] = useState(stringValue(target.value.serviceDeskId));
  const [requestTypeId, setRequestTypeId] = useState(stringValue(target.value.requestTypeId));
  const [requestTypeName, setRequestTypeName] = useState(stringValue(target.value.requestTypeName));
  const chooseRequestType = (id: string) => {
    setRequestTypeId(id);
    setRequestTypeName(requestTypes.find((candidate) => candidate.id === id)?.name ?? "");
  };
  const isComplete = serviceDeskId.trim() === "" && requestTypeId.trim() === "" || isNumericID(serviceDeskId) && isNumericID(requestTypeId);
  const canList = isCloudID(listingCloudId);
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <ListingSiteField connectionCloudId={connectionCloudId} listingCloudId={listingCloudId} onChange={setListingCloudId}/>
    <div className="studio-actions">
      <StudioButton disabled={busy || !canList} onClick={() => onChooseServiceDesk(listingCloudId.toLowerCase())}>Choose service desk</StudioButton>
      <StudioButton disabled={busy || !canList || !isNumericID(serviceDeskId)} onClick={() => onChooseRequestType(listingCloudId.toLowerCase(), serviceDeskId.trim())}>Choose request type</StudioButton>
      {requestTypeName && <span className="studio-muted"><strong>Request type:</strong> {requestTypeName}</span>}
    </div>
    {serviceDesks.length > 0 && <StudioField label="Service desk">
      <select onChange={(event) => { setServiceDeskId(event.target.value); setRequestTypeId(""); setRequestTypeName(""); }} value={serviceDeskId}>
        <option value="">Select a service desk</option>
        {serviceDesks.map((desk) => <option key={desk.id} value={desk.id}>{desk.name} ({desk.projectKey})</option>)}
      </select>
    </StudioField>}
    {requestTypes.length > 0 && <StudioField label="Request type">
      <select onChange={(event) => chooseRequestType(event.target.value)} value={requestTypeId}>
        <option value="">Select a request type</option>
        {requestTypes.map((requestType) => <option key={requestType.id} value={requestType.id}>{requestType.name}</option>)}
      </select>
    </StudioField>}
    {isListTruncated && <StudioNotice tone="attention">Jira Service Management returned more entries than one list can show. Enter the IDs below to use one that is not listed.</StudioNotice>}
    <StudioField hint={serviceDeskIDHint} label="Service desk ID">
      <input onChange={(event) => { setServiceDeskId(event.target.value.trim()); setRequestTypeName(""); }} placeholder="10" type="text" value={serviceDeskId}/>
    </StudioField>
    <StudioField hint="Numeric request type ID, such as 25, which Choose request type fills in. To find it by hand, open https://<your-site>.atlassian.net/rest/servicedeskapi/servicedesk/<service desk ID>/requesttype while signed in; each request type's id is listed beside its name. Leave both fields blank to use each Start Flow input's requestTypeId." label="Request type ID">
      <input onChange={(event) => { setRequestTypeId(event.target.value.trim()); setRequestTypeName(""); }} placeholder="25" type="text" value={requestTypeId}/>
    </StudioField>
    {!isComplete && <StudioNotice tone="error">Enter both a numeric service desk ID and a numeric request type ID, or leave both blank.</StudioNotice>}
    <SaveAction
      disabled={busy || !isComplete || (target.required && requestTypeId.trim() === "")}
      onSave={() => onSave({serviceDeskId: serviceDeskId.trim(), requestTypeId: requestTypeId.trim(), requestTypeName})}
    />
  </StudioSurface>;
}

function ListingSiteField({connectionCloudId, listingCloudId, onChange}: {connectionCloudId: string; listingCloudId: string; onChange(value: string): void}) {
  if (connectionCloudId !== "") return null;
  return <StudioField hint={"Used only to list service desks and request types; the connection's own cloudId still selects the site. " + tenantInfoHint} label="Site cloudId for listing">
    <input onChange={(event) => onChange(event.target.value.trim())} placeholder="1324a887-45db-1bf4-1e99-ef0ff456d421" type="text" value={listingCloudId}/>
  </StudioField>;
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
