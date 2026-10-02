// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import {
  StudioButton,
  StudioHeader,
  StudioNotice,
  StudioSurface,
  type ConnectorStudioConfigurationUnitTarget,
} from "@superdurable/dex-connectors-react";
import { useState, type ReactNode } from "react";

/** PickedList is one loaded picker list; isTruncated reports that paging stopped before Graph's last page. */
export interface PickedList<T> { items: T[]; isTruncated: boolean; }

export function UnitFrame({target, loadError, children}: {target: ConnectorStudioConfigurationUnitTarget; loadError?: string; children: ReactNode}) {
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    {children}
  </StudioSurface>;
}

export function TruncatedNotice({isTruncated, noun}: {isTruncated: boolean; noun: string}) {
  if (!isTruncated) return null;
  return <StudioNotice tone="attention">
    Microsoft Graph returned more {noun} than one list can show. Enter an ID to use one that is not listed.
  </StudioNotice>;
}

export function SaveAction({disabled, onSave}: {disabled?: boolean; onSave(): Promise<unknown> | void}) {
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

export function stringValue(value: unknown): string { return typeof value === "string" ? value : ""; }
