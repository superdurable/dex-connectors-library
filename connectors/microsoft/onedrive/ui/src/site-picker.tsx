// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StudioButton, StudioField, StudioNotice, type ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { useState } from "react";
import { siteIDParts, type SharePointSite } from "./provider.js";
import { SaveAction, TruncatedNotice, UnitFrame, stringValue, type PickedList } from "./unit-frame.js";

export interface SitePickerProps {
  target: ConnectorStudioConfigurationUnitTarget;
  sites?: PickedList<SharePointSite>;
  busy?: boolean;
  loadError?: string;
  onSearchSites(query: string): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function SitePickerUnit({target, sites, busy, loadError, onSearchSites, onSave}: SitePickerProps) {
  const [query, setQuery] = useState("");
  const [siteId, setSiteId] = useState(stringValue(target.value.siteId));
  const [siteName, setSiteName] = useState(stringValue(target.value.siteName));
  const listed = sites?.items ?? [];
  const isSiteIDInvalid = siteId.trim() !== "" && !siteIDParts(siteId);
  const editSiteID = (value: string) => {
    setSiteId(value);
    setSiteName(listed.find((site) => site.id === value.trim())?.name ?? "");
  };
  return <UnitFrame loadError={loadError} target={target}>
    <StudioField hint="Search by a word in the site name, such as Finance. Microsoft Graph searches only sites you can open." label="Search SharePoint sites">
      <input onChange={(event) => setQuery(event.target.value)} type="search" value={query}/>
    </StudioField>
    <div className="studio-actions">
      <StudioButton disabled={busy || query.trim() === ""} onClick={() => onSearchSites(query.trim())}>Search sites</StudioButton>
      {siteName && <span className="studio-muted"><strong>Site:</strong> {siteName}</span>}
    </div>
    {listed.length > 0 && <StudioField label="Site">
      <select onChange={(event) => editSiteID(event.target.value)} value={siteId.trim()}>
        <option value="">My OneDrive (no site)</option>
        {listed.map((site) => <option key={site.id} value={site.id}>{site.name}</option>)}
      </select>
    </StudioField>}
    {sites && listed.length === 0 && <StudioNotice tone="info">No site matched. Try another word, or paste the site ID.</StudioNotice>}
    <TruncatedNotice isTruncated={sites?.isTruncated ?? false} noun="sites"/>
    <StudioField hint="Paste a Graph site ID of the form hostname,site-collection-GUID,web-GUID from Graph Explorer when site search is not allowed, as with Sites.Selected. Leave blank for your own OneDrive." label="Site ID">
      <input onChange={(event) => editSiteID(event.target.value)} type="text" value={siteId}/>
    </StudioField>
    {isSiteIDInvalid && <StudioNotice tone="error">Enter a site ID of the form contoso.sharepoint.com,GUID,GUID.</StudioNotice>}
    <SaveAction
      disabled={busy || isSiteIDInvalid || (target.required && siteId.trim() === "")}
      onSave={() => onSave({siteId: siteId.trim(), siteName: siteId.trim() === "" ? "" : siteName})}
    />
  </UnitFrame>;
}
