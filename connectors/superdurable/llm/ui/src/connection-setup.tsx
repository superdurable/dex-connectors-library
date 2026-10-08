// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { useEffect, useMemo, useState, type ReactElement } from "react";
import {
  StudioButton,
  StudioField,
  StudioHeader,
  StudioNotice,
  StudioSurface,
  filterModelOptions,
  saveConnectorConnection,
  withDraftCredentials,
  type ConnectionSetupProps,
  type ModelListing,
} from "@superdurable/dex-connectors-react";

import { loadProviderModels, validateLLMModelID, type LLMProvider, type LLMRegion } from "./model-selection.js";
import { findLLMProviderSetup, llmProviderSetups, type LLMProviderSetup } from "./provider-catalog.js";

const apiKeyField = "api_key";
const otherModelOption = "__other_model__";
// Wait this long after the last keystroke before listing models with a typed key.
const typedKeyListingDelayMilliseconds = 700;

type ModelListState =
  | {status: "waitingForKey"}
  | {status: "loading"}
  | {status: "loaded"; listing: ModelListing}
  | {status: "failed"; message: string};

type SaveState = {status: "idle"} | {status: "saving"} | {status: "failed"; message: string};

/**
 * LLMConnectionSetup is the llm connection's whole setup: the provider, its API
 * key and region or workspace, a model from the provider's live list, and one
 * Save. The key is sent only to Dex Web; Dex Web lists models with it and
 * stores it. It renders a fresh form whenever the saved connection changes.
 */
export function LLMConnectionSetup({client, ready}: ConnectionSetupProps): ReactElement {
  const [hasSaved, setHasSaved] = useState(false);
  const {configuration, storedCredentialFields} = ready.connection;
  const formKey = JSON.stringify([ready.sessionNonce, configuration, storedCredentialFields]);
  return <StudioSurface label="LLM connection">
    <StudioHeader
      description="Choose the provider, enter its API key, and pick a model. Every Step on this connection uses this provider."
      iconUrl="./icon.svg" title="LLM connection"
    />
    {hasSaved && <StudioNotice tone="success">Saved. Restart the application to use the new settings.</StudioNotice>}
    <LLMConnectionForm client={client} key={formKey} onSaved={() => setHasSaved(true)} ready={ready}/>
  </StudioSurface>;
}

function LLMConnectionForm({client, ready, onSaved}: ConnectionSetupProps & {onSaved(): void}): ReactElement {
  const saved = ready.connection.configuration;
  const savedSetup = findLLMProviderSetup(saved.provider);
  const [provider, setProvider] = useState<LLMProvider | "">(savedSetup?.provider ?? "");
  const [region, setRegion] = useState<LLMRegion>(readSavedRegion(saved.region));
  const [workspaceId, setWorkspaceId] = useState(typeof saved.anthropicWorkspaceId === "string" ? saved.anthropicWorkspaceId : "");
  const [apiKey, setAPIKey] = useState("");
  const [listedKey, setListedKey] = useState("");
  const [model, setModel] = useState(typeof saved.model === "string" ? saved.model.trim() : "");
  const [isEnteringModelID, setEnteringModelID] = useState(false);
  const [isShowingAllModels, setShowingAllModels] = useState(false);
  const [listState, setListState] = useState<ModelListState>({status: "waitingForKey"});
  const [listAttempt, setListAttempt] = useState(0);
  const [saveState, setSaveState] = useState<SaveState>({status: "idle"});

  const setup = findLLMProviderSetup(provider);
  // A saved key belongs to the saved provider; switching provider needs that provider's key.
  const canKeepSavedKey = ready.connection.storedCredentialFields.includes(apiKeyField) && provider !== "" && provider === savedSetup?.provider;
  const typedKey = apiKey.trim();

  useEffect(() => {
    const timer = window.setTimeout(() => setListedKey(typedKey), typedKeyListingDelayMilliseconds);
    return () => window.clearTimeout(timer);
  }, [typedKey]);

  useEffect(() => {
    if (provider === "" || (listedKey === "" && !canKeepSavedKey)) {
      setListState({status: "waitingForKey"});
      return undefined;
    }
    let isCurrent = true;
    setListState({status: "loading"});
    const listingClient = withDraftCredentials(client, listedKey === "" ? {} : {[apiKeyField]: listedKey});
    loadProviderModels(listingClient, provider, region).then(
      (listing) => { if (isCurrent) setListState({status: "loaded", listing}); },
      (error: unknown) => { if (isCurrent) setListState({status: "failed", message: error instanceof Error ? error.message : "Unknown error"}); },
    );
    return () => { isCurrent = false; };
    // client changes identity on every render; a new session arrives as a new form key.
  }, [provider, region, listedKey, canKeepSavedKey, listAttempt]);

  const listedModels = listState.status === "loaded" ? listState.listing.models : [];
  const visibleModels = useMemo(() => filterModelOptions(listedModels, "", isShowingAllModels), [listedModels, isShowingAllModels]);
  const hiddenModelCount = listedModels.filter((option) => option.isHiddenByDefault).length;
  const isModelListed = model === "" || listedModels.some((option) => option.id === model);
  const showsModelIDEntry = isEnteringModelID || (listState.status !== "loading" && !isModelListed);
  const modelProblem = model === "" || isModelListed ? undefined : validateLLMModelID(model);
  const keyProblem = provider !== "" && typedKey === "" && !canKeepSavedKey ? `Enter the ${setup?.label} API key.` : undefined;
  const canSave = provider !== "" && keyProblem === undefined && modelProblem === undefined && saveState.status !== "saving";

  const chooseProvider = (next: string) => {
    const nextSetup = findLLMProviderSetup(next);
    // A typed key belongs to the provider it was typed for; never list another provider's models with it.
    setAPIKey("");
    setListedKey("");
    setProvider(nextSetup?.provider ?? "");
    setRegion(nextSetup?.provider === savedSetup?.provider ? readSavedRegion(saved.region) : "global");
    setModel(nextSetup?.provider === savedSetup?.provider && typeof saved.model === "string" ? saved.model.trim() : "");
    setEnteringModelID(false);
    setSaveState({status: "idle"});
  };
  const chooseModelOption = (value: string) => {
    if (value === otherModelOption) {
      setEnteringModelID(true);
      setModel("");
      return;
    }
    setEnteringModelID(false);
    setModel(value);
  };
  const save = () => {
    if (!setup) return;
    setSaveState({status: "saving"});
    saveConnectorConnection(client, {
      configuration: connectionConfiguration(setup, region, model, workspaceId, saved),
      credentials: typedKey === "" ? {} : {[apiKeyField]: typedKey},
      keepCredentialFields: typedKey === "" && canKeepSavedKey ? [apiKeyField] : [],
    }).then(
      () => { setSaveState({status: "idle"}); onSaved(); },
      (error: unknown) => setSaveState({status: "failed", message: error instanceof Error ? error.message : "Unknown error"}),
    );
  };

  return <>
    <StudioField hint="Every model call on this connection goes to this provider. To use two providers, add a second llm connection." label="Provider">
      <select onChange={(event) => chooseProvider(event.target.value)} value={provider}>
        <option value="">Choose a provider</option>
        {llmProviderSetups.map((option) => <option key={option.provider} value={option.provider}>{option.label}</option>)}
      </select>
    </StudioField>
    {setup && <>
      <StudioField hint={apiKeyHint(setup, canKeepSavedKey)} label={`${setup.label} API key`}>
        <input
          autoComplete="off" onChange={(event) => setAPIKey(event.target.value)}
          placeholder={canKeepSavedKey ? "Saved key (leave blank to keep it)" : "Paste the API key"}
          spellCheck={false} type="password" value={apiKey}
        />
      </StudioField>
      {setup.regions.length > 1 && <StudioField
        hint="The platform where the key was created; a key works only on its own platform. Models are listed from it."
        label="Region"
      >
        <select onChange={(event) => setRegion(readSavedRegion(event.target.value))} value={region}>
          {setup.regions.map((choice) => <option key={choice.region} value={choice.region}>{choice.label}</option>)}
        </select>
      </StudioField>}
      {setup.provider === "anthropic" && <StudioField
        hint="Only for a key that spans several Claude workspaces: the wrkspc_ ID from Claude Console > Settings > Workspaces. Leave blank for a key scoped to one workspace."
        label="Workspace ID (optional)"
      >
        <input onChange={(event) => setWorkspaceId(event.target.value)} placeholder="wrkspc_…" spellCheck={false} type="text" value={workspaceId}/>
      </StudioField>}
      <StudioField hint={modelHint(setup)} label="Model">
        <select
          disabled={listState.status === "waitingForKey" || listState.status === "loading"}
          onChange={(event) => chooseModelOption(event.target.value)}
          value={showsModelIDEntry ? otherModelOption : model}
        >
          <option value="">Provider default ({setup.defaultModel})</option>
          {visibleModels.map((option) => <option key={option.id} value={option.id}>
            {option.label && option.label !== option.id ? `${option.label} (${option.id})` : option.id}
          </option>)}
          <option value={otherModelOption}>Another model ID…</option>
        </select>
      </StudioField>
      {listState.status === "waitingForKey" && <StudioNotice tone="info">Enter the API key to list {setup.label} models.</StudioNotice>}
      {listState.status === "loading" && <StudioNotice tone="info">Loading models from {setup.label}…</StudioNotice>}
      {listState.status === "failed" && <>
        <StudioNotice tone="error">{listState.message} You can still enter a model ID.</StudioNotice>
        <div className="studio-actions"><StudioButton onClick={() => setListAttempt((attempt) => attempt + 1)}>Retry</StudioButton></div>
      </>}
      {listState.status === "loaded" && listState.listing.notices?.map((notice, index) =>
        <StudioNotice key={index} tone={notice.tone}>{notice.message}</StudioNotice>)}
      {listState.status === "loaded" && listState.listing.isTruncated && <StudioNotice tone="attention">
        {setup.label} returned more models than the list shows. Choose Another model ID to enter one.
      </StudioNotice>}
      {hiddenModelCount > 0 && <label className="studio-checkbox">
        <input checked={isShowingAllModels} onChange={(event) => setShowingAllModels(event.target.checked)} type="checkbox"/>
        Show all models ({hiddenModelCount} hidden that may not generate text)
      </label>}
      {showsModelIDEntry && <StudioField hint="Any model ID the provider serves, written exactly as the provider names it, without a provider prefix." label="Model ID">
        <input onChange={(event) => setModel(event.target.value.trim())} placeholder="model-id" spellCheck={false} type="text" value={model}/>
      </StudioField>}
      {modelProblem !== undefined && <StudioNotice tone="attention">{modelProblem}</StudioNotice>}
    </>}
    <div className="studio-actions">
      <StudioButton disabled={!canSave} onClick={save} variant="primary">{saveState.status === "saving" ? "Saving…" : "Save"}</StudioButton>
      {keyProblem !== undefined && <span className="studio-muted">{keyProblem}</span>}
    </div>
    {saveState.status === "failed" && <StudioNotice tone="error">The connection could not be saved: {saveState.message}</StudioNotice>}
  </>;
}

/** connectionConfiguration is the complete configuration Save stores; a field it omits is cleared. */
function connectionConfiguration(
  setup: LLMProviderSetup, region: LLMRegion, model: string, workspaceId: string, saved: Record<string, unknown>,
): Record<string, unknown> {
  const configuration: Record<string, unknown> = {provider: setup.provider};
  if (setup.regions.length > 1) configuration.region = region;
  if (model !== "") configuration.model = model;
  if (setup.provider === "anthropic" && workspaceId.trim() !== "") configuration.anthropicWorkspaceId = workspaceId.trim();
  // maxResponseBytes has no field here; keep a value someone set for this provider.
  if (saved.provider === setup.provider && saved.maxResponseBytes !== undefined) configuration.maxResponseBytes = saved.maxResponseBytes;
  return configuration;
}

function apiKeyHint(setup: LLMProviderSetup, canKeepSavedKey: boolean): string {
  const hint = `Create ${setup.keyFormat} at ${setup.keyPage} (${setup.keyPagePath}). It is stored only as this connection's secret and sent only to ${setup.apiHost}.`;
  return canKeepSavedKey ? `${hint} Leave blank to keep the saved key.` : hint;
}

function modelHint(setup: LLMProviderSetup): string {
  return `Listed live from ${setup.label} with this key. The provider default applies when you pick none, and each Step can still choose its own model.`;
}

function readSavedRegion(value: unknown): LLMRegion {
  return value === "us" || value === "eu" || value === "china" || value === "hong-kong" ? value : "global";
}
