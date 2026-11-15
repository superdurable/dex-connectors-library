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
import { boardIDFromEntry, savedBoardID } from "./board.js";

export interface MondayUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  busy?: boolean;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function MondayConfigurationUnit(props: MondayUnitProps) {
  switch (props.target.unitId) {
    case "boardIdInput": return <BoardIDInputUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported monday.com configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function BoardIDInputUnit({target, busy, onSave}: MondayUnitProps) {
  const [entry, setEntry] = useState(savedBoardID(target.value.boardId));
  const boardID = boardIDFromEntry(entry);
  const isBlank = entry.trim() === "";
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    <StudioField
      hint="Open the board at https://monday.com/ and copy the digits after /boards/ in the address bar, or paste the whole board address. Only the numeric board ID is saved; it is not secret."
      label="Board ID"
    >
      <input inputMode="numeric" onChange={(event) => setEntry(event.target.value)} placeholder="1234567890" type="text" value={entry}/>
    </StudioField>
    {!isBlank && boardID === "" && <StudioNotice tone="error">Enter the numeric board ID, such as 1234567890, or a board address such as https://example.monday.com/boards/1234567890.</StudioNotice>}
    {isBlank && <p className="studio-muted">Blank saves nothing: the binding has no board and the application refuses to start its Trigger.</p>}
    <SaveAction disabled={busy || boardID === ""} onSave={() => onSave({boardId: boardID})}/>
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
