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
import { isSalesforceAPIName, standardSObjects } from "./provider.js";

export interface SalesforceUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  busy?: boolean;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function SalesforceConfigurationUnit(props: SalesforceUnitProps) {
  if (props.target.unitId === "sObjectPicker") return <SObjectPickerUnit {...props}/>;
  if (props.target.unitId === "fieldNameInput") return <FieldNameInputUnit {...props}/>;
  const {target} = props;
  return <StudioNotice tone="error">Unsupported Salesforce configuration unit: {target.unitId}</StudioNotice>;
}

function SObjectPickerUnit({target, busy, onSave}: SalesforceUnitProps) {
  const [objectInput, setObjectInput] = useState(stringValue(target.value.sObjectType));
  const sObjectType = objectInput.trim();
  const isInvalid = sObjectType !== "" && !isSalesforceAPIName(sObjectType);
  const listedObject = standardSObjects.find((object) => object.apiName === sObjectType);
  return <UnitFrame target={target}>
    <StudioField label="Standard object">
      <select onChange={(event) => setObjectInput(event.target.value)} value={listedObject ? listedObject.apiName : ""}>
        <option value="">{target.required ? "Select an object" : "No object; the Flow decides"}</option>
        {standardSObjects.map((object) => <option key={object.apiName} value={object.apiName}>{object.label}</option>)}
      </select>
    </StudioField>
    <StudioField hint="Custom objects are not listed. Enter the API name from Setup > Object Manager > the object > API Name, such as Invoice__c. Leave blank to let the Flow decide." label="Object API name">
      <input onChange={(event) => setObjectInput(event.target.value)} type="text" value={objectInput}/>
    </StudioField>
    {isInvalid && <StudioNotice tone="error">Enter an object API name such as Contact or Invoice__c, without spaces.</StudioNotice>}
    <SaveAction disabled={busy || isInvalid || (target.required && sObjectType === "")} onSave={() => onSave({sObjectType})}/>
  </UnitFrame>;
}

function FieldNameInputUnit({target, busy, onSave}: SalesforceUnitProps) {
  const [fieldInput, setFieldInput] = useState(stringValue(target.value.fieldName));
  const fieldName = fieldInput.trim();
  const isInvalid = fieldName !== "" && !isSalesforceAPIName(fieldName);
  return <UnitFrame target={target}>
    <StudioField hint="Copy the Field Name column from Setup > Object Manager > the object > Fields & Relationships, such as Email or ERP_Id__c. Custom fields end in __c." label="Field API name">
      <input onChange={(event) => setFieldInput(event.target.value)} type="text" value={fieldInput}/>
    </StudioField>
    {isInvalid && <StudioNotice tone="error">Enter a field API name such as Email or ERP_Id__c, without spaces or relationship dots.</StudioNotice>}
    <SaveAction disabled={busy || isInvalid || (target.required && fieldName === "")} onSave={() => onSave({fieldName})}/>
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

function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
