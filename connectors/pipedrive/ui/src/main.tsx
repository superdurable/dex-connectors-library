// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  applyConnectorStudioTheme,
  collectProviderPages,
  observeConnectorStudioFrameAutoHeight,
  useConnectorStudioClient,
} from "@superdurable/dex-connectors-react";
import { PipedriveSetupView } from "./setup.js";
import { PipedriveConfigurationUnit } from "./units.js";
import {
  joinPipelineStages,
  oauthAuthMethodID,
  parsePipedriveFieldPage,
  parsePipedrivePipelinePage,
  parsePipedriveStagePage,
  parsePipedriveUsers,
  pipedriveObjectTypes,
  type PipedriveCustomField,
  type PipedrivePipeline,
  type PipedriveUser,
} from "./provider.js";

const connectorId = "pipedrive";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [owners, setOwners] = useState<PipedriveUser[]>([]);
  const [pipelines, setPipelines] = useState<PipedrivePipeline[]>([]);
  const [customFields, setCustomFields] = useState<PipedriveCustomField[]>([]);
  const [customFieldObjectType, setCustomFieldObjectType] = useState("");
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  const loadOwners = async () => {
    setLoadError(undefined);
    try {
      setOwners(parsePipedriveUsers(await client.executeProviderCommand("listUsers", "pipedrive.users-list")));
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Pipedrive users could not be loaded");
    }
  };
  const loadPipelines = async () => {
    setLoadError(undefined);
    try {
      const pipelinePages = await collectProviderPages(async (cursor) => {
        const page = parsePipedrivePipelinePage(await client.executeProviderCommand("listPipelines", "pipedrive.stages-list", cursor ? {cursor} : {}));
        return {items: page.items, nextCursor: page.nextCursor};
      });
      const stagePages = await collectProviderPages(async (cursor) => {
        const page = parsePipedriveStagePage(await client.executeProviderCommand("listStages", "pipedrive.stages-list", cursor ? {cursor} : {}));
        return {items: page.items, nextCursor: page.nextCursor};
      });
      setPipelines(joinPipelineStages(pipelinePages.items, stagePages.items));
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Pipedrive pipelines could not be loaded");
    }
  };
  const loadCustomFields = async (objectType: string) => {
    setLoadError(undefined);
    const objectTypeOption = pipedriveObjectTypes.find((option) => option.id === objectType);
    if (!objectTypeOption) return;
    try {
      const {items} = await collectProviderPages(async (cursor) => {
        const page = parsePipedriveFieldPage(await client.executeProviderCommand(objectTypeOption.command, "pipedrive.fields-list", cursor ? {cursor} : {}));
        return {items: page.items, nextCursor: page.nextCursor};
      });
      setCustomFields(items);
      setCustomFieldObjectType(objectType);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Pipedrive custom fields could not be loaded");
    }
  };

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") {
    return <PipedriveSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  return <PipedriveConfigurationUnit
    busy={client.busy}
    canListFromPipedrive={!ready.connection.authMethodIds.includes(oauthAuthMethodID)}
    customFieldObjectType={customFieldObjectType}
    customFields={customFields}
    loadError={loadError}
    onLoadCustomFields={(objectType) => void loadCustomFields(objectType)}
    onLoadOwners={() => void loadOwners()}
    onLoadPipelines={() => void loadPipelines()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    owners={owners}
    pipelines={pipelines}
    target={ready.target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
