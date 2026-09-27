// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { useCallback, useEffect, useState, type ReactElement } from "react";

import type { ConnectorStudioConfigurationUnitTarget } from "./host-api.js";
import { StudioButton, StudioField, StudioHeader, StudioNotice, StudioSurface } from "./studio-components.js";

/** ModelOption is one model a provider listed, projected by connector UI code. */
export interface ModelOption {
  /** id is the exact model ID sent to the provider. */
  id: string;
  /** label is a human-readable name; the ID is shown when it is absent. */
  label?: string;
  /** detail is one short line, such as a context window or a deprecation date. */
  detail?: string;
  /** badges are short capability tags, such as "structured output". */
  badges?: string[];
  /**
   * isHiddenByDefault marks a listed model that the connector's filter judged
   * unsuitable for this operation, such as an embedding model. "Show all
   * models" reveals it.
   */
  isHiddenByDefault?: boolean;
}

/** ModelListing is the result of one live model list. */
export interface ModelListing {
  models: ModelOption[];
  /** isTruncated reports that paging stopped before the provider's last page. */
  isTruncated?: boolean;
}

/** ModelPickerProps configures the shared live model picker unit. */
export interface ModelPickerProps {
  /** target is the configuration unit target; its value.model is the saved pick. */
  target: ConnectorStudioConfigurationUnitTarget;
  /** providerName names the provider in messages, such as "Gemini". */
  providerName: string;
  /** loadModels lists models through the host broker; it runs on mount and on Retry. */
  loadModels(): Promise<ModelListing>;
  /**
   * onSave persists the pick through use.configuration.save. An empty model
   * means "use the connection's default model".
   */
  onSave(value: {model: string}): Promise<unknown>;
}

type ListState =
  | {status: "loading"}
  | {status: "loaded"; listing: ModelListing}
  | {status: "failed"; message: string};

type SaveState = {status: "idle"} | {status: "saving"} | {status: "saved"; model: string} | {status: "failed"; message: string};

/**
 * ModelPicker lists a provider's models live, so a model the provider adds
 * appears without a connector release. The user can pick a listed model, keep
 * the connection's default, or type any model ID when listing fails or a model
 * is missing from the list.
 */
export function ModelPicker({target, providerName, loadModels, onSave}: ModelPickerProps): ReactElement {
  const initialModel = savedModel(target);
  const [listState, setListState] = useState<ListState>({status: "loading"});
  const [saveState, setSaveState] = useState<SaveState>({status: "idle"});
  const [query, setQuery] = useState("");
  const [isShowingAll, setShowingAll] = useState(false);
  const [selectedModel, setSelectedModel] = useState(initialModel);
  const [manualModel, setManualModel] = useState("");

  const load = useCallback(() => {
    setListState({status: "loading"});
    loadModels().then(
      (listing) => {
        setListState({status: "loaded", listing});
        if (initialModel !== "" && !listing.models.some((model) => model.id === initialModel)) setManualModel(initialModel);
      },
      (error: unknown) => {
        setListState({status: "failed", message: error instanceof Error ? error.message : "Unknown error"});
        if (initialModel !== "") setManualModel(initialModel);
      },
    );
  }, [initialModel, loadModels]);
  useEffect(load, [load]);

  const models = listState.status === "loaded" ? listState.listing.models : [];
  const visibleModels = filterModelOptions(models, query, isShowingAll);
  const hiddenCount = models.filter((model) => model.isHiddenByDefault).length;
  const chooseListedModel = (model: string) => {
    setSelectedModel(model);
    setManualModel("");
  };
  const save = () => {
    setSaveState({status: "saving"});
    onSave({model: selectedModel}).then(
      () => setSaveState({status: "saved", model: selectedModel}),
      (error: unknown) => setSaveState({status: "failed", message: error instanceof Error ? error.message : "Unknown error"}),
    );
  };

  return <StudioSurface label={target.label}>
    <StudioHeader title={target.label} description={target.description ?? `Choose the ${providerName} model this Step calls.`}/>
    {listState.status === "loading" && <StudioNotice tone="info">Loading models from {providerName}…</StudioNotice>}
    {listState.status === "failed" && <>
      <StudioNotice tone="error">{providerName} models could not be listed: {listState.message} Enter a model ID below, or retry.</StudioNotice>
      <div className="studio-actions"><StudioButton onClick={load}>Retry</StudioButton></div>
    </>}
    {listState.status === "loaded" && <>
      <StudioField label="Search models"><input onChange={(event) => setQuery(event.target.value)} placeholder="Name or ID" type="search" value={query}/></StudioField>
      {hiddenCount > 0 && <label className="studio-checkbox">
        <input checked={isShowingAll} onChange={(event) => setShowingAll(event.target.checked)} type="checkbox"/>
        Show all models ({hiddenCount} hidden that may not support this Step)
      </label>}
      {listState.listing.isTruncated && <StudioNotice tone="attention">{providerName} returned more models than one list can show. Search, or enter a model ID.</StudioNotice>}
    </>}
    <fieldset aria-label="Model" className="studio-options">
      <label className="studio-option">
        <input checked={selectedModel === ""} name="model" onChange={() => chooseListedModel("")} type="radio" value=""/>
        <span className="studio-option-label">Use the connection's default model</span>
      </label>
      {visibleModels.map((model) => <label className="studio-option" key={model.id}>
        <input checked={selectedModel === model.id} name="model" onChange={() => chooseListedModel(model.id)} type="radio" value={model.id}/>
        <span className="studio-option-label">{model.label || model.id}</span>
        {model.label && model.label !== model.id && <span className="studio-option-id">{model.id}</span>}
        {model.detail && <span className="studio-option-detail">{model.detail}</span>}
        {model.badges && model.badges.length > 0 && <span className="studio-badges">{model.badges.map((badge) => <span className="studio-badge" key={badge}>{badge}</span>)}</span>}
      </label>)}
    </fieldset>
    <StudioField hint="Any model ID the provider accepts, including one missing from the list." label="Or enter a model ID">
      <input onChange={(event) => {
        setManualModel(event.target.value);
        setSelectedModel(event.target.value.trim());
      }} placeholder="model-id" type="text" value={manualModel}/>
    </StudioField>
    <div className="studio-actions">
      <StudioButton disabled={saveState.status === "saving"} onClick={save} variant="primary">Save</StudioButton>
      <span className="studio-muted">{selectedModel === "" ? "Uses the connection's default model" : `Selected: ${selectedModel}`}</span>
    </div>
    {saveState.status === "saved" && <StudioNotice tone="success">Saved {saveState.model || "the connection default"}. Restart the application to use it.</StudioNotice>}
    {saveState.status === "failed" && <StudioNotice tone="error">The model could not be saved: {saveState.message}</StudioNotice>}
  </StudioSurface>;
}

/**
 * filterModelOptions keeps models whose ID or label contains query, ignoring
 * case, and drops isHiddenByDefault models unless isShowingAll is true. It
 * keeps the provider's order.
 */
export function filterModelOptions(models: ModelOption[], query: string, isShowingAll: boolean): ModelOption[] {
  const needle = query.trim().toLowerCase();
  return models.filter((model) =>
    (isShowingAll || !model.isHiddenByDefault)
    && (needle === "" || model.id.toLowerCase().includes(needle) || (model.label ?? "").toLowerCase().includes(needle)));
}

/** savedModel returns the unit's saved model ID, or "" when it inherits the connection default. */
export function savedModel(target: ConnectorStudioConfigurationUnitTarget): string {
  return typeof target.value.model === "string" ? target.value.model.trim() : "";
}
