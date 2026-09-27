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

export interface GmailUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  busy?: boolean;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function GmailConfigurationUnit(props: GmailUnitProps) {
  const {target} = props;
  if (target.unitId === "emailListInput") return <EmailListUnit {...props}/>;
  if (target.unitId === "searchQueryInput") return <TextUnit {...props} placeholder="label:inbox" port="query"/>;
  if (target.unitId === "textInput") return <TextUnit {...props} port="text"/>;
  return <StudioNotice tone="error">Unsupported Gmail configuration unit: {target.unitId}</StudioNotice>;
}

function EmailListUnit({target, busy, onSave}: GmailUnitProps) {
  const [value, setValue] = useState(stringArray(target.value.emails).join(", "));
  const emails = emailList(value);
  return <UnitFrame target={target}>
    <StudioField hint="Separate addresses with commas." label={target.label}>
      <input onChange={(event) => setValue(event.target.value)} placeholder="person@example.com" type="text" value={value}/>
    </StudioField>
    <SaveAction disabled={busy || (target.required && emails.length === 0)} onSave={() => onSave({emails})}/>
  </UnitFrame>;
}

function TextUnit({target, busy, port, placeholder, onSave}: GmailUnitProps & {port: "query" | "text"; placeholder?: string}) {
  const [value, setValue] = useState(stringValue(target.value[port]));
  return <UnitFrame target={target}>
    <StudioField label={target.label}>
      <input onChange={(event) => setValue(event.target.value)} placeholder={placeholder} type="text" value={value}/>
    </StudioField>
    <SaveAction disabled={busy || (target.required && value.length === 0)} onSave={() => onSave({[port]: value})}/>
  </UnitFrame>;
}

function UnitFrame({target, children}: {target: ConnectorStudioConfigurationUnitTarget; children: ReactNode}) {
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
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

function emailList(value: string): string[] { return value.split(",").map((item) => item.trim()).filter(Boolean); }
function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
function stringArray(value: unknown): string[] { return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : []; }
