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
import { isCloudID, isSpaceKey, type ConfluenceSite, type ConfluenceSpace } from "./provider.js";

const tenantInfoHint = "Sign in to Confluence, open https://<your-site>.atlassian.net/_edge/tenant_info in the same browser, and copy its cloudId: a UUID such as 1324a887-45db-1bf4-1e99-ef0ff456d421.";

export interface SitePickerUnitProps {
  target: ConnectorStudioConnectionTarget;
  sites?: ConfluenceSite[];
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
  return <StudioSurface label="Confluence site">
    <StudioHeader description="Choose the Confluence Cloud site every operation of this connection uses. Leave it blank to use the only Confluence site the authorization grants." title="Confluence site"/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <div className="studio-actions"><StudioButton disabled={busy} onClick={onChooseSite}>Choose Confluence site</StudioButton></div>
    {sites.length > 0 && <StudioField label="Site">
      <select onChange={(event) => setCloudId(event.target.value)} value={cloudId}>
        <option value="">Select a Confluence site</option>
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

export interface SpacePickerUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  /** connectionCloudId is the connection's saved site, or "" when the host does not report it. */
  connectionCloudId: string;
  spaces?: ConfluenceSpace[];
  isSpaceListTruncated?: boolean;
  busy?: boolean;
  loadError?: string;
  onChooseSpace(cloudId: string): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function ConfluenceConfigurationUnit(props: SpacePickerUnitProps) {
  if (props.target.unitId === "spacePicker") return <SpacePickerUnit {...props}/>;
  return <StudioNotice tone="error">Unsupported Confluence configuration unit: {props.target.unitId}</StudioNotice>;
}

function SpacePickerUnit({target, connectionCloudId, spaces = [], isSpaceListTruncated, busy, loadError, onChooseSpace, onSave}: SpacePickerUnitProps) {
  const [listingCloudId, setListingCloudId] = useState(connectionCloudId);
  const [spaceKey, setSpaceKey] = useState(stringValue(target.value.spaceKey));
  const [space, setSpace] = useState<ConfluenceSpace | undefined>(savedSpace(target));
  const trimmedSpaceKey = spaceKey.trim();
  const isSpaceKeyValid = trimmedSpaceKey === "" || isSpaceKey(trimmedSpaceKey);
  const canList = isCloudID(listingCloudId);
  const choose = (key: string) => {
    setSpaceKey(key);
    setSpace(spaces.find((candidate) => candidate.key === key));
  };
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    {connectionCloudId === "" && <StudioField hint={"Used only to list spaces; the connection's own cloudId still selects the site. " + tenantInfoHint} label="Confluence site cloudId">
      <input onChange={(event) => setListingCloudId(event.target.value.trim())} placeholder="1324a887-45db-1bf4-1e99-ef0ff456d421" type="text" value={listingCloudId}/>
    </StudioField>}
    <div className="studio-actions">
      <StudioButton disabled={busy || !canList} onClick={() => onChooseSpace(listingCloudId.toLowerCase())}>Choose space</StudioButton>
      {space && <span className="studio-muted"><strong>Space:</strong> {space.name} ({space.key})</span>}
    </div>
    {spaces.length > 0 && <StudioField label="Space">
      <select onChange={(event) => choose(event.target.value)} value={spaceKey}>
        <option value="">Select a space</option>
        {spaces.map((candidate) => <option key={candidate.id} value={candidate.key}>{candidate.name} ({candidate.key})</option>)}
      </select>
    </StudioField>}
    {isSpaceListTruncated && <StudioNotice tone="attention">
      Confluence returned more spaces than one list can show. Enter the space key to use one that is not listed.
    </StudioNotice>}
    <StudioField hint="Space key shown in Confluence > Spaces > the space > Space settings, such as OPS; a personal space key starts with ~. Leave it blank to use each Start Flow input's spaceKey." label="Space key">
      <input onChange={(event) => { setSpaceKey(event.target.value); setSpace(undefined); }} placeholder="OPS" type="text" value={spaceKey}/>
    </StudioField>
    {!isSpaceKeyValid && <StudioNotice tone="error">The space key must be letters, digits, underscores, or hyphens, optionally starting with ~.</StudioNotice>}
    <SaveAction
      disabled={busy || !isSpaceKeyValid || (target.required && trimmedSpaceKey === "")}
      onSave={() => onSave({spaceId: space?.key === trimmedSpaceKey ? space.id : "", spaceKey: trimmedSpaceKey, spaceName: space?.key === trimmedSpaceKey ? space.name : ""})}
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

function describeSite(site: ConfluenceSite): string {
  return site.url ? `${site.name} (${site.url})` : site.name;
}

function savedSpace(target: ConnectorStudioConfigurationUnitTarget): ConfluenceSpace | undefined {
  const key = stringValue(target.value.spaceKey);
  if (key === "") return undefined;
  return {id: stringValue(target.value.spaceId), key, name: stringValue(target.value.spaceName) || key};
}

function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
