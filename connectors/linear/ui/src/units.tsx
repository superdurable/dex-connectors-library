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
import { isLinearTeamUUID } from "./provider.js";

export interface LinearTeam { id: string; key: string; name: string; }

export interface LinearUnitProps {
  target: ConnectorStudioConfigurationUnitTarget;
  teams: LinearTeam[];
  isTeamListTruncated?: boolean;
  busy?: boolean;
  loadError?: string;
  onLoadTeams(): void;
  onSave(value: Record<string, unknown>): Promise<unknown> | void;
}

export function LinearConfigurationUnit(props: LinearUnitProps) {
  switch (props.target.unitId) {
    case "teamPicker": return <TeamPickerUnit {...props}/>;
    default: return <StudioNotice tone="error">Unsupported Linear configuration unit: {props.target.unitId}</StudioNotice>;
  }
}

function TeamPickerUnit({target, teams, isTeamListTruncated, busy, loadError, onLoadTeams, onSave}: LinearUnitProps) {
  const [team, setTeam] = useState<LinearTeam>({
    id: stringValue(target.value.teamId), key: stringValue(target.value.teamKey), name: stringValue(target.value.teamName),
  });
  const isValid = team.id === "" ? !target.required : isLinearTeamUUID(team.id);
  const selectTeam = (teamId: string) => setTeam(teams.find((candidate) => candidate.id === teamId) ?? {id: "", key: "", name: ""});
  return <StudioSurface label={target.label}>
    <StudioHeader description={target.description} title={target.label}/>
    {loadError && <StudioNotice tone="error">{loadError}</StudioNotice>}
    <div className="studio-actions">
      <StudioButton disabled={busy} onClick={onLoadTeams}>Load teams</StudioButton>
      <span className="studio-muted">Lists the teams an OAuth connection can see; a personal API key uses the team UUID below.</span>
    </div>
    {isTeamListTruncated && <StudioNotice tone="attention">Only the first 100 teams are listed; enter another team's UUID below.</StudioNotice>}
    <StudioField label="Team">
      <select onChange={(event) => selectTeam(event.target.value)} value={teams.some((candidate) => candidate.id === team.id) ? team.id : ""}>
        <option value="">{target.required ? "Select a team" : "Every team"}</option>
        {teams.map((candidate) => <option key={candidate.id} value={candidate.id}>{candidate.name} ({candidate.key})</option>)}
      </select>
    </StudioField>
    <StudioField hint="In Linear, open Settings > Teams, choose the team, and use Copy team ID; the value looks like 2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4." label="Team UUID fallback">
      <input onChange={(event) => setTeam({id: event.target.value.trim(), key: "", name: ""})} placeholder="2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4" type="text" value={team.id}/>
    </StudioField>
    {!isValid && team.id !== "" && <StudioNotice tone="error">Enter a team UUID with five hyphen-separated hexadecimal groups.</StudioNotice>}
    <SaveAction disabled={busy || !isValid} onSave={() => onSave({teamId: team.id.toLowerCase(), teamKey: team.key, teamName: team.name})}/>
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
