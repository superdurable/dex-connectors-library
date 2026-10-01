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
import { HubSpotSetupView } from "./setup.js";
import { HubSpotConfigurationUnit } from "./units.js";
import { parseHubSpotDealPipelines, parseHubSpotOwnerPage, type HubSpotDealPipeline, type HubSpotOwner } from "./provider.js";

const connectorId = "hubspot";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [owners, setOwners] = useState<HubSpotOwner[]>([]);
  const [isOwnerListTruncated, setIsOwnerListTruncated] = useState(false);
  const [pipelines, setPipelines] = useState<HubSpotDealPipeline[]>([]);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  const loadOwners = async () => {
    setLoadError(undefined);
    try {
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const page = parseHubSpotOwnerPage(await client.executeProviderCommand("listOwners", "hubspot.owners-list", cursor ? {after: cursor} : {}));
        return {items: page.owners, nextCursor: page.nextCursor};
      });
      setOwners(items);
      setIsOwnerListTruncated(isTruncated);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "HubSpot owners could not be loaded");
    }
  };
  const loadPipelines = async () => {
    setLoadError(undefined);
    try {
      setPipelines(parseHubSpotDealPipelines(await client.executeProviderCommand("listDealPipelines", "hubspot.pipelines-list")));
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "HubSpot deal pipelines could not be loaded");
    }
  };

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") {
    return <HubSpotSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  return <HubSpotConfigurationUnit
    busy={client.busy}
    isOwnerListTruncated={isOwnerListTruncated}
    loadError={loadError}
    onLoadOwners={() => void loadOwners()}
    onLoadPipelines={() => void loadPipelines()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    owners={owners}
    pipelines={pipelines}
    target={ready.target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
