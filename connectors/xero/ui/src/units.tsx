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
import {
  invoiceStatuses,
  invoiceTypes,
  readInvoiceChangeFilter,
  savedInvoiceChangeFilter,
  type InvoiceStatus,
  type InvoiceType,
} from "./filter.js";

export interface XeroUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  busy?: boolean;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function XeroConfigurationUnit(props: XeroUnitProps) {
  switch (props.target.unitId) {
    case "invoiceChangeFilter": return <InvoiceChangeFilterUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported Xero configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function InvoiceChangeFilterUnit({target, busy, onSave}: XeroUnitProps) {
  const saved = readInvoiceChangeFilter(target.value);
  const [invoiceType, setInvoiceType] = useState<InvoiceType>(saved.type);
  const [statuses, setStatuses] = useState<InvoiceStatus[]>(saved.statuses);
  const toggleStatus = (status: InvoiceStatus, isChecked: boolean) =>
    setStatuses((current) => isChecked ? [...current, status] : current.filter((value) => value !== status));
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    <StudioField hint="Xero calls sales invoices ACCREC and supplier bills ACCPAY, under Business > Invoices and Business > Bills to pay at https://go.xero.com." label="Invoice type">
      <select onChange={(event) => setInvoiceType(event.target.value as InvoiceType)} value={invoiceType}>
        {invoiceTypes.map((choice) => <option key={choice.value} value={choice.value}>{choice.label}</option>)}
      </select>
    </StudioField>
    <StudioField hint="Only changes that leave an invoice in a checked status start the Flow; check none to receive every status. PAID is set when the amount due reaches zero." label="Status after the change">
      <div className="studio-actions">
        {invoiceStatuses.map((status) => <label key={status}>
          <input checked={statuses.includes(status)} onChange={(event) => toggleStatus(status, event.target.checked)} type="checkbox"/> {status}
        </label>)}
      </div>
    </StudioField>
    {invoiceType === "" && statuses.length === 0 && <StudioNotice tone="info">Every invoice and bill created or changed starts the Flow.</StudioNotice>}
    <SaveAction disabled={busy} onSave={() => onSave(savedInvoiceChangeFilter({type: invoiceType, statuses}))}/>
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
    {saveState.status === "saved" && <StudioNotice tone="success">Saved. Restart the application; a changed filter records a new baseline.</StudioNotice>}
    {saveState.status === "failed" && <StudioNotice tone="error">The configuration could not be saved: {saveState.message}</StudioNotice>}
  </>;
}
