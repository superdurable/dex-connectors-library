// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StrictMode, useCallback, useEffect, type ReactElement } from "react";
import { createRoot } from "react-dom/client";

import { observeConnectorStudioFrameAutoHeight } from "./frame-auto-height.js";
import { ModelPicker, type ModelListing } from "./model-picker.js";
import { StudioHeader, StudioNotice, StudioSurface } from "./studio-components.js";
import { useConnectorStudioClient, type ConnectorStudioClient, type ConnectorStudioConnection } from "./studio-client.js";
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
   * provider's JSON. It runs for a configurationUnit target and for a
   * connection target that renders the modelPicker unit, again on Retry, and
   * again when the session or connection.authMethodIds change. connection is
   * the ready message's connection; a loader that combines providers skips
   * each provider whose auth method the connection has not added, with
   * shouldListModelsForAuthMethod, which lists every provider when the host
   * reports no auth methods.
   */
  loadModels(client: ConnectorStudioClient, connection: ConnectorStudioConnection): Promise<ModelListing>;
  /**
   * validateManualModel returns a message for an invalid typed model ID, or
   * undefined to accept it. It is passed to ModelPicker, which disables Save
   * while the message shows; see ModelPickerProps.validateManualModel.
   */
  validateManualModel?(model: string): string | undefined;
  /** manualModelPlaceholder is passed to ModelPicker; it defaults to "model-id". */
  manualModelPlaceholder?: string;
  /**
   * defaultModelDescription names the model the connector uses when the
   * connection saves no model, such as "first added provider's default model".
   * A Step picker names the connection's configuration.model in its first
   * option, "Connection default (anthropic/claude-sonnet-5)", and names this
   * description when the host reports a configuration without a model. The
   * connection's own picker labels its empty option "Connector default
   * (<description>)". Without it, and on hosts that report no configuration,
   * the options keep their generic labels.
   */
  defaultModelDescription?: string;
}

/**
 * ModelPickerStudioApp renders a whole LLM connector bundle: a connection
 * status card for the connection surface, ModelPicker for the modelPicker
 * unit of a Step or Trigger, and ModelPicker for the connection's model field
 * when the host renders the modelPicker unit in the connection form.
 * Credentials stay in the host form.
 */
export function ModelPickerStudioApp({
  connectorId, providerName, iconUrl, loadModels, validateManualModel, manualModelPlaceholder, defaultModelDescription,
}: ModelPickerBundleConfig): ReactElement {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);
  // The client object changes every render; the picker reloads only when the session or its auth methods do.
  const loadModelsForSession = useCallback(async () => {
    if (!ready) throw new Error("Dex Web has not sent the connection");
    return loadModels(client, ready.connection);
  }, [ready?.sessionNonce, ready?.connection.authMethodIds.join("\n")]);

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  const {target} = ready;
  if (target.kind === "connection" && target.unitId === undefined) {
    return <StudioSurface label={`${providerName} connection`}>
      <StudioHeader description="Enter the API key in the form above. Each Flow Step chooses its model below." iconUrl={iconUrl} title={providerName}/>
      {ready.connection.state === "connected"
        ? <StudioNotice tone="success">Connected. Models are listed live from {providerName}.</StudioNotice>
        : <StudioNotice tone="info">Not connected yet.</StudioNotice>}
    </StudioSurface>;
  }
  if (target.unitId !== modelPickerUnitID) {
    return <StudioNotice tone="error">Unsupported {providerName} configuration unit: {target.unitId}</StudioNotice>;
  }
  return <ModelPicker
    defaultModelOptionLabel={target.kind === "connection"
      ? describeConnectorDefaultOption(defaultModelDescription)
      : describeConnectionDefaultOption(ready.connection, defaultModelDescription)}
    loadModels={loadModelsForSession}
    manualModelPlaceholder={manualModelPlaceholder}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    providerName={providerName}
    target={target}
    validateManualModel={validateManualModel}
  />;
}

/** mountModelPickerBundle renders ModelPickerStudioApp into the bundle's #root element. */
export function mountModelPickerBundle(config: ModelPickerBundleConfig): void {
  const root = document.getElementById("root");
  if (!root) throw new Error("Studio bundle has no #root element");
  createRoot(root).render(<StrictMode><ModelPickerStudioApp {...config}/></StrictMode>);
}

function describeConnectionDefaultOption(connection: ConnectorStudioConnection, defaultModelDescription: string | undefined): string | undefined {
  const connectionModel = typeof connection.configuration.model === "string" ? connection.configuration.model.trim() : "";
  if (connectionModel !== "") return `Connection default (${connectionModel})`;
  const description = defaultModelDescription?.trim() ?? "";
  // A host that reports no configuration may still hold a connection model, so the description could mislead.
  return connection.isConfigurationReported && description !== "" ? `Connection default (${description})` : undefined;
}

function describeConnectorDefaultOption(defaultModelDescription: string | undefined): string | undefined {
  const description = defaultModelDescription?.trim() ?? "";
  return description === "" ? undefined : `Connector default (${description})`;
}
