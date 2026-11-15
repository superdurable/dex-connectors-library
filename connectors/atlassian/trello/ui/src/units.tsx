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
import { boardIdFromInput } from "./board.js";

export interface TrelloUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  busy?: boolean;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function TrelloConfigurationUnit(props: TrelloUnitProps) {
  if (props.target.unitId === "boardInput") return <BoardInputUnit {...props}/>;
  return <StudioNotice tone="error">Unsupported Trello configuration unit: {props.target.unitId}</StudioNotice>;
}

/** BoardInputUnit saves a board's short link from a pasted board link, or a board ID as entered. */
function BoardInputUnit({target, busy, onSave}: TrelloUnitProps) {
  const saved = typeof target.value.boardId === "string" ? target.value.boardId : "";
  const [input, setInput] = useState(saved);
  const boardId = boardIdFromInput(input);
  const isInvalid = input.trim() !== "" && boardId === undefined;
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    <StudioField hint="Open the board at https://trello.com and copy its address, https://trello.com/b/<short link>/<board name>, or paste the 24-character board ID." label={target.label}>
      <input onChange={(event) => setInput(event.target.value)} placeholder="https://trello.com/b/AbCd1234/support-queue" type="text" value={input}/>
    </StudioField>
    {boardId && <p className="studio-muted">Saves board <strong>{boardId}</strong>.</p>}
    {isInvalid && <StudioNotice tone="error">Paste a board link that starts with https://trello.com/b/, an 8-character short link, or a 24-character board ID; a card link does not identify a board.</StudioNotice>}
    <SaveAction disabled={busy || boardId === undefined} onSave={() => onSave({boardId})}/>
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
