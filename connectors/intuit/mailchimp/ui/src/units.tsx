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
import { audienceIdProblem, memberStatuses, selectedSupportedStatuses } from "./statuses.js";

export interface MailchimpUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  busy?: boolean;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function MailchimpConfigurationUnit(props: MailchimpUnitProps) {
  switch (props.target.unitId) {
    case "audienceIdEntry": return <AudienceIdEntryUnit {...props}/>;
    case "memberStatusPicker": return <MemberStatusPickerUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported Mailchimp configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function AudienceIdEntryUnit({target, busy, onSave}: MailchimpUnitProps) {
  const [audienceId, setAudienceId] = useState(typeof target.value.audienceId === "string" ? target.value.audienceId : "");
  const trimmed = audienceId.trim();
  const problem = trimmed === "" && !target.required ? undefined : audienceIdProblem(trimmed);
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    <StudioField
      hint="In Mailchimp choose Audience, pick the audience in the Audience drop-down if you have several, then More options > Audience settings and copy Audience ID. It is not secret."
      label="Audience ID"
    >
      <input
        aria-invalid={problem !== undefined && trimmed !== ""}
        autoComplete="off"
        onChange={(change) => setAudienceId(change.target.value)}
        placeholder="57afe96172"
        spellCheck={false}
        type="text"
        value={audienceId}
      />
    </StudioField>
    <p className="studio-muted">
      Open <a href="https://login.mailchimp.com/" rel="noreferrer" target="_blank">Mailchimp</a> or see{" "}
      <a href="https://mailchimp.com/help/find-audience-id/" rel="noreferrer" target="_blank">Find your audience ID</a>.
      Mailchimp serves audiences only from your API key&#x27;s data center, so they cannot be listed here.
    </p>
    {problem !== undefined && trimmed !== "" && <StudioNotice tone="attention">{problem}</StudioNotice>}
    <SaveAction disabled={busy || problem !== undefined} onSave={() => onSave({audienceId: trimmed})}/>
  </StudioSurface>;
}

function MemberStatusPickerUnit({target, busy, onSave}: MailchimpUnitProps) {
  const [statuses, setStatuses] = useState(selectedSupportedStatuses(target.value.statuses));
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    <fieldset aria-label="Contact statuses" className="studio-options">
      {memberStatuses.map(({status, description}) => <label className="studio-option" key={status}>
        <input checked={statuses.includes(status)} onChange={(change) => setStatuses(change.target.checked
          ? selectedSupportedStatuses([...statuses, status]) : statuses.filter((selected) => selected !== status))} type="checkbox"/>
        <span className="studio-option-label">{description}</span>
        <span className="studio-option-id">{status}</span>
      </label>)}
    </fieldset>
    <p className="studio-muted">Select none to deliver changed contacts in every status. A contact is matched on its status after the change.</p>
    <SaveAction disabled={busy || (target.required && statuses.length === 0)} onSave={() => onSave({statuses})}/>
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
