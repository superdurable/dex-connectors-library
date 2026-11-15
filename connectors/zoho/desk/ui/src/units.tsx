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
import { describeOrganization, isDepartmentID, isOrganizationID, type ZohoDeskDepartment, type ZohoDeskOrganization } from "./provider.js";

const manualOrganizationHint = "Sign in to Zoho Desk in the connection's data center, open Setup > Developer Space > API, and copy the organization ID: digits such as 2389290.";

export interface OrganizationPickerUnitProps {
  target: ConnectorStudioConnectionTarget;
  /** canListOrganizations is false when the host does not report the connection's data center. */
  canListOrganizations: boolean;
  organizations?: ZohoDeskOrganization[];
  busy?: boolean;
  loadError?: string;
  onChooseOrganization(): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

/** OrganizationPickerUnit renders the connection's orgId field when the host shows the organizationPicker unit inline. */
export function OrganizationPickerUnit({target, canListOrganizations, organizations = [], busy, loadError, onChooseOrganization, onSave}: OrganizationPickerUnitProps) {
  const [orgId, setOrgId] = useState(stringValue(target.value?.orgId));
  const trimmedOrgId = orgId.trim();
  const isValid = isOrganizationID(trimmedOrgId);
  return <StudioSurface label="Zoho Desk organization">
    <StudioHeader description="Choose the Zoho Desk organization every operation of this connection uses: the one you picked on the Zoho consent screen. The application does not start while it is blank." title="Zoho Desk organization"/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    {canListOrganizations
      ? <div className="studio-actions"><StudioButton disabled={busy} onClick={onChooseOrganization}>Choose Zoho Desk organization</StudioButton></div>
      : <StudioNotice tone="attention">Dex Web did not report the connection's data center, so organizations cannot be listed. Enter the orgId below.</StudioNotice>}
    {organizations.length > 0 && <StudioField label="Organization">
      <select onChange={(event) => setOrgId(event.target.value)} value={orgId}>
        <option value="">Select an organization</option>
        {organizations.map((organization) => <option key={organization.orgId} value={organization.orgId}>{describeOrganization(organization)}</option>)}
      </select>
    </StudioField>}
    <StudioField hint={manualOrganizationHint} label="orgId">
      <input onChange={(event) => setOrgId(event.target.value)} placeholder="2389290" type="text" value={orgId}/>
    </StudioField>
    {trimmedOrgId !== "" && !isValid && <StudioNotice tone="error">The orgId must be the organization's numeric ID; a portal name or URL is not accepted.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave({orgId: trimmedOrgId})}/>
  </StudioSurface>;
}

const manualDepartmentHint = "Sign in to Zoho Desk in the connection's data center, open Setup > General > Departments, open the department, and copy the long numeric ID shown in the browser address bar, such as 1892000000006907. Leave it blank to watch every department the connection's agent can see.";

export interface DepartmentPickerUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  /** canListDepartments is false when the host reports no data center or the connection has no saved orgId. */
  canListDepartments: boolean;
  departments?: ZohoDeskDepartment[];
  isDepartmentListTruncated?: boolean;
  busy?: boolean;
  loadError?: string;
  onChooseDepartment(): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

/** DepartmentPickerUnit saves the departmentId, plus a listed department's name, for one Trigger binding or Step. */
export function DepartmentPickerUnit({target, canListDepartments, departments = [], isDepartmentListTruncated, busy, loadError, onChooseDepartment, onSave}: DepartmentPickerUnitProps) {
  const [departmentId, setDepartmentId] = useState(stringValue(target.value.departmentId));
  const trimmedDepartmentId = departmentId.trim();
  const isValid = trimmedDepartmentId === "" ? !target.required : isDepartmentID(trimmedDepartmentId);
  const listed = departments.find((department) => department.departmentId === trimmedDepartmentId);
  const savedName = trimmedDepartmentId === stringValue(target.value.departmentId) ? stringValue(target.value.departmentName) : "";
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    {canListDepartments
      ? <div className="studio-actions"><StudioButton disabled={busy} onClick={onChooseDepartment}>Choose Zoho Desk department</StudioButton></div>
      : <StudioNotice tone="attention">Departments can be listed only after the connection is Ready and its organization is saved. Enter the department ID below, or leave it blank.</StudioNotice>}
    {departments.length > 0 && <StudioField label="Department">
      <select onChange={(event) => setDepartmentId(event.target.value)} value={listed ? trimmedDepartmentId : ""}>
        <option value="">{target.required ? "Select a department" : "Every department"}</option>
        {departments.map((department) => <option key={department.departmentId} value={department.departmentId}>{department.name} ({department.departmentId})</option>)}
      </select>
    </StudioField>}
    {isDepartmentListTruncated && <StudioNotice tone="attention">Only the first departments are listed. Enter the department ID below when yours is missing.</StudioNotice>}
    <StudioField hint={manualDepartmentHint} label="departmentId">
      <input onChange={(event) => setDepartmentId(event.target.value)} placeholder="1892000000006907" type="text" value={departmentId}/>
    </StudioField>
    {trimmedDepartmentId !== "" && !isDepartmentID(trimmedDepartmentId) && <StudioNotice tone="error">The department ID must be Zoho Desk's numeric ID; a department name is not accepted.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave({departmentId: trimmedDepartmentId, departmentName: listed?.name ?? savedName})}/>
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
    {saveState.status === "failed" && <StudioNotice tone="error">The value could not be saved: {saveState.message}</StudioNotice>}
  </>;
}

function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
