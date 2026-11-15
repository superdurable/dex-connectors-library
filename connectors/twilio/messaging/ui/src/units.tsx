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
import { isReceivingAddress } from "./address.js";

export interface TwilioUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  busy?: boolean;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function TwilioConfigurationUnit(props: TwilioUnitProps) {
  switch (props.target.unitId) {
    case "receivingAddressInput": return <ReceivingAddressUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported Twilio configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

// Studio commands cannot send Twilio's HTTP Basic credential, so the number is typed rather than listed.
function ReceivingAddressUnit({target, busy, onSave}: TwilioUnitProps) {
  const [address, setAddress] = useState(stringValue(target.value.to));
  const isValid = address === "" ? !target.required : isReceivingAddress(address);
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    <StudioField
      hint="Open https://console.twilio.com/us1/develop/phone-numbers/manage/incoming (Phone Numbers > Manage > Active numbers) and copy the number, or Messaging > Senders > WhatsApp senders and prefix it with whatsapp:. Not secret. Blank receives every inbound message of the account."
      label="Receiving number">
      <input onChange={(event) => setAddress(event.target.value.trim())} placeholder="+14155550100" type="text" value={address}/>
    </StudioField>
    {!isValid && address !== "" && <StudioNotice tone="error">Enter an E.164 number such as +14155550100, or whatsapp:+14155550100.</StudioNotice>}
    {!isValid && address === "" && <StudioNotice tone="error">This binding requires a receiving number.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave(address === "" ? {} : {to: address})}/>
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
    {saveState.status === "saved" && <StudioNotice tone="success">Saved. Restart the Worker to use it; a changed number records a new baseline.</StudioNotice>}
    {saveState.status === "failed" && <StudioNotice tone="error">The configuration could not be saved: {saveState.message}</StudioNotice>}
  </>;
}

function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
