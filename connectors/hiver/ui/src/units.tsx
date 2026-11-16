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
import { parseInboxID } from "./provider.js";

export interface HiverInbox { id: string; name: string; email?: string; }

export interface HiverUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  inboxes: HiverInbox[];
  isInboxListTruncated?: boolean;
  busy?: boolean;
  loadError?: string;
  onLoadInboxes(): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function HiverConfigurationUnit(props: HiverUnitProps) {
  switch (props.target.unitId) {
    case "inboxPicker": return <InboxPickerUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported Hiver configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function InboxPickerUnit({target, inboxes, isInboxListTruncated, busy, loadError, onLoadInboxes, onSave}: HiverUnitProps) {
  const [inboxID, setInboxID] = useState(savedInboxID(target.value.inboxId));
  const parsedInboxID = parseInboxID(inboxID);
  const isValid = inboxID === "" ? !target.required : parsedInboxID !== undefined;
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadInboxes}>Load inboxes</StudioButton>
      <span className="studio-muted">Lists the account's shared inboxes with the connection's saved API key, 100 per request.</span>
    </div>
    {isInboxListTruncated && <StudioNotice tone="attention">Only the first inboxes are listed; enter another inbox ID below.</StudioNotice>}
    <StudioField label="Shared inbox">
      <select onChange={(event) => setInboxID(event.target.value)} value={inboxID}>
        <option value="">{target.required ? "Select a shared inbox" : "No inbox"}</option>
        {inboxes.map((inbox) => <option key={inbox.id} value={inbox.id}>
          {inbox.name}{inbox.email ? ` · ${inbox.email}` : ""}
        </option>)}
      </select>
    </StudioField>
    <StudioField hint="Use the inbox ID when it is not listed: the id field of the inbox in GET https://api2.hiverhq.com/v1/inboxes, letters, digits, hyphens, or underscores." label="Inbox ID fallback">
      <input onChange={(event) => setInboxID(event.target.value.trim())} placeholder="105902" type="text" value={inboxID}/>
    </StudioField>
    {!isValid && inboxID !== "" && <StudioNotice tone="error">Enter a Hiver inbox ID of 1 to 64 letters, digits, hyphens, or underscores.</StudioNotice>}
    {!isValid && inboxID === "" && <StudioNotice tone="info">Select or enter a shared inbox; it cannot be blank.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave(parsedInboxID === undefined ? {} : {inboxId: parsedInboxID})}/>
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

function savedInboxID(value: unknown): string {
  if (typeof value === "number" && Number.isSafeInteger(value) && value > 0) return String(value);
  return typeof value === "string" && parseInboxID(value) !== undefined ? value : "";
}
