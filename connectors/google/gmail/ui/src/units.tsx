import type { ConnectorStudioConfigurationUnitTarget } from "@superdurable/dex-connectors-react";
import { useState, type ReactNode } from "react";

export interface GmailUnitProps { target: ConnectorStudioConfigurationUnitTarget; onSave(value: Record<string, unknown>): void; }

export function GmailConfigurationUnit({target, onSave}: GmailUnitProps) {
  if (target.unitId === "emailListInput") return <EmailListUnit target={target} onSave={onSave}/>;
  if (target.unitId === "searchQueryInput") return <TextUnit target={target} port="query" placeholder="label:inbox" onSave={onSave}/>;
  if (target.unitId === "textInput") return <TextUnit target={target} port="text" onSave={onSave}/>;
  return <p role="alert">Unsupported Gmail configuration unit: {target.unitId}</p>;
}

function EmailListUnit({target, onSave}: GmailUnitProps) {
  const [value, setValue] = useState(stringArray(target.value.emails).join(", "));
  const emails = emailList(value);
  return <UnitFrame target={target}><label>{target.label}<input value={value} placeholder="person@example.com" onChange={(event) => setValue(event.target.value)}/></label><button disabled={target.required && emails.length === 0} onClick={() => onSave({emails})} type="button">Save</button></UnitFrame>;
}

function TextUnit({target, port, placeholder, onSave}: GmailUnitProps & {port: "query" | "text"; placeholder?: string}) {
  const [value, setValue] = useState(stringValue(target.value[port]));
  return <UnitFrame target={target}><label>{target.label}<input value={value} placeholder={placeholder} onChange={(event) => setValue(event.target.value)}/></label><button disabled={target.required && value.length === 0} onClick={() => onSave({[port]: value})} type="button">Save</button></UnitFrame>;
}

function UnitFrame({target, children}: {target: ConnectorStudioConfigurationUnitTarget; children: ReactNode}) {
  return <section className="unit"><h2>{target.label}</h2>{target.description && <p>{target.description}</p>}{children}</section>;
}

function emailList(value: string): string[] { return value.split(",").map((item) => item.trim()).filter(Boolean); }
function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
function stringArray(value: unknown): string[] { return Array.isArray(value) ? value.filter((item): item is string => typeof item === "string") : []; }
