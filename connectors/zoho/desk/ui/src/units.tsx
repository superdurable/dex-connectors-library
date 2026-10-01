// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StudioButton,
  StudioField,
  StudioHeader,
  StudioNotice,
  StudioSurface,
  type ConnectorStudioConnectionTarget,
} from "@superdurable/dex-connectors-react";
import { useState } from "react";
import { describeOrganization, isOrganizationID, type ZohoDeskOrganization } from "./provider.js";

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
    {saveState.status === "failed" && <StudioNotice tone="error">The organization could not be saved: {saveState.message}</StudioNotice>}
  </>;
}

function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
