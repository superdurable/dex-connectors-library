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
import { useState, type ReactNode } from "react";
import { isIntercomAdminID, type IntercomAdmin } from "./admins.js";
import { conversationEventTopics, selectedSupportedTopics } from "./topics.js";

export interface IntercomUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  admins: IntercomAdmin[];
  busy?: boolean;
  loadError?: string;
  onLoadAdmins(): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function IntercomConfigurationUnit(props: IntercomUnitProps) {
  switch (props.target.unitId) {
    case "adminPicker": return <AdminPickerUnit {...props}/>;
    case "conversationTopicPicker": return <ConversationTopicPickerUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported Intercom configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function AdminPickerUnit({target, admins, busy, loadError, onLoadAdmins, onSave}: IntercomUnitProps) {
  const [adminId, setAdminId] = useState(stringValue(target.value.adminId));
  const isValid = adminId === "" ? !target.required : isIntercomAdminID(adminId);
  return <UnitFrame loadError={loadError} target={target}>
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadAdmins}>Load admins</StudioButton>
      <span className="studio-muted">Lists the teammates in the connection's Intercom workspace.</span>
    </div>
    <StudioField label="Admin">
      <select onChange={(event) => setAdminId(event.target.value)} value={adminId}>
        <option value="">Select an admin</option>
        {admins.map((admin) => <option key={admin.id} value={admin.id}>{admin.name}{admin.email ? ` · ${admin.email}` : ""}{admin.isAway ? " (away)" : ""} · {admin.id}</option>)}
      </select>
    </StudioField>
    <StudioField hint="When the list cannot load, open the teammate's profile in Intercom and copy the number at the end of its page address, such as …/admins/5017691." label="Admin ID fallback">
      <input onChange={(event) => setAdminId(event.target.value.trim())} placeholder="5017691" type="text" value={adminId}/>
    </StudioField>
    {!isValid && adminId !== "" && <StudioNotice tone="error">An Intercom admin ID is a string of digits.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave({adminId})}/>
  </UnitFrame>;
}

function ConversationTopicPickerUnit({target, busy, onSave}: IntercomUnitProps) {
  const [topics, setTopics] = useState(selectedSupportedTopics(target.value.topics));
  return <UnitFrame target={target}>
    <fieldset aria-label="Conversation topics" className="studio-options">
      {conversationEventTopics.map(({topic, description}) => <label className="studio-option" key={topic}>
        <input checked={topics.includes(topic)} onChange={(event) => setTopics(event.target.checked
          ? selectedSupportedTopics([...topics, topic]) : topics.filter((selected) => selected !== topic))} type="checkbox"/>
        <span className="studio-option-label">{description}</span>
        <span className="studio-option-id">{topic}</span>
      </label>)}
    </fieldset>
    <p className="studio-muted">Select none to accept every topic listed. Intercom sends only the topics subscribed on the app's Configure &gt; Webhooks page.</p>
    <SaveAction disabled={busy || (target.required && topics.length === 0)} onSave={() => onSave({topics})}/>
  </UnitFrame>;
}

function UnitFrame({target, loadError, children}: {target: ConnectorStudioConfigurationUnitTarget; loadError?: string; children: ReactNode}) {
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    {children}
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

function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
