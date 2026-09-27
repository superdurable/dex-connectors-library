// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StrictMode, useCallback, useEffect, type ReactElement } from "react";
import { createRoot } from "react-dom/client";

import { observeConnectorStudioFrameAutoHeight } from "./frame-auto-height.js";
import { ModelPicker, type ModelListing } from "./model-picker.js";
import { StudioHeader, StudioNotice, StudioSurface } from "./studio-components.js";
import { useConnectorStudioClient, type ConnectorStudioClient } from "./studio-client.js";
import { applyConnectorStudioTheme } from "./studio-theme.js";

/** modelPickerUnitID is the Studio unit ID every LLM connector declares for its model picker. */
export const modelPickerUnitID = "modelPicker";

/** ModelPickerBundleConfig describes one LLM connector's Studio bundle. */
export interface ModelPickerBundleConfig {
  /** connectorId is the manifest connector ID, such as "gemini". */
  connectorId: string;
  /** providerName names the provider in the UI, such as "Gemini". */
  providerName: string;
  /** iconUrl is a bundle-relative icon, such as "./icon.svg". */
  iconUrl?: string;
  /**
   * loadModels lists models with client.executeProviderCommand and projects the
   * provider's JSON. It runs only for a configurationUnit target.
   */
  loadModels(client: ConnectorStudioClient): Promise<ModelListing>;
}

/**
 * ModelPickerStudioApp renders a whole LLM connector bundle: a connection
 * status card for the connection surface and ModelPicker for the modelPicker
 * unit. Credentials stay in the host form.
 */
export function ModelPickerStudioApp({connectorId, providerName, iconUrl, loadModels}: ModelPickerBundleConfig): ReactElement {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);
  // The client object changes every render; the picker reloads only when the session does.
  const loadModelsForSession = useCallback(() => loadModels(client), [ready?.sessionNonce]);

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") {
    return <StudioSurface label={`${providerName} connection`}>
      <StudioHeader description="Enter the API key in the form above. Each Flow Step chooses its model below." iconUrl={iconUrl} title={providerName}/>
      {ready.connection.state === "connected"
        ? <StudioNotice tone="success">Connected. Models are listed live from {providerName}.</StudioNotice>
        : <StudioNotice tone="info">Not connected yet.</StudioNotice>}
    </StudioSurface>;
  }
  if (ready.target.unitId !== modelPickerUnitID) {
    return <StudioNotice tone="error">Unsupported {providerName} configuration unit: {ready.target.unitId}</StudioNotice>;
  }
  return <ModelPicker
    loadModels={loadModelsForSession}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    providerName={providerName}
    target={ready.target}
  />;
}

/** mountModelPickerBundle renders ModelPickerStudioApp into the bundle's #root element. */
export function mountModelPickerBundle(config: ModelPickerBundleConfig): void {
  const root = document.getElementById("root");
  if (!root) throw new Error("Studio bundle has no #root element");
  createRoot(root).render(<StrictMode><ModelPickerStudioApp {...config}/></StrictMode>);
}
