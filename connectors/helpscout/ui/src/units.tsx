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
import { parseMailboxID } from "./provider.js";

export interface HelpScoutMailbox { id: number; name: string; email?: string; }

export interface HelpScoutUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  mailboxes: HelpScoutMailbox[];
  isMailboxListTruncated?: boolean;
  busy?: boolean;
  loadError?: string;
  onLoadMailboxes(): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function HelpScoutConfigurationUnit(props: HelpScoutUnitProps) {
  switch (props.target.unitId) {
    case "mailboxPicker": return <MailboxPickerUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported Help Scout configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function MailboxPickerUnit({target, mailboxes, isMailboxListTruncated, busy, loadError, onLoadMailboxes, onSave}: HelpScoutUnitProps) {
  const [mailboxID, setMailboxID] = useState(savedMailboxID(target.value.mailboxId));
  const parsedMailboxID = parseMailboxID(mailboxID);
  const isValid = mailboxID === "" ? !target.required : parsedMailboxID !== undefined;
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadMailboxes}>Load inboxes</StudioButton>
      <span className="studio-muted">Lists the inboxes the app's Help Scout user can access, once the application has stored an access token.</span>
    </div>
    {isMailboxListTruncated && <StudioNotice tone="attention">Only the first inboxes are listed; enter another inbox ID below.</StudioNotice>}
    <StudioField label="Inbox">
      <select onChange={(event) => setMailboxID(event.target.value)} value={mailboxID}>
        <option value="">{target.required ? "Select an inbox" : "Every inbox"}</option>
        {mailboxes.map((mailbox) => <option key={mailbox.id} value={String(mailbox.id)}>
          {mailbox.name}{mailbox.email ? ` · ${mailbox.email}` : ""}
        </option>)}
      </select>
    </StudioField>
    <StudioField hint="Use the numeric inbox ID when it is not listed: the id field of the inbox in GET https://api.helpscout.net/v2/mailboxes, not its slug." label="Inbox ID fallback">
      <input inputMode="numeric" onChange={(event) => setMailboxID(event.target.value.trim())} placeholder="12345" type="text" value={mailboxID}/>
    </StudioField>
    {!isValid && mailboxID !== "" && <StudioNotice tone="error">Enter a positive whole-number inbox ID.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave(parsedMailboxID === undefined ? {} : {mailboxId: parsedMailboxID})}/>
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

function savedMailboxID(value: unknown): string {
  return typeof value === "number" && Number.isSafeInteger(value) && value > 0 ? String(value) : "";
}
