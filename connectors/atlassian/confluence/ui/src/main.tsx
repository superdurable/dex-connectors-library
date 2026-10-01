// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  StudioNotice,
  applyConnectorStudioTheme,
  collectProviderPages,
  observeConnectorStudioFrameAutoHeight,
  useConnectorStudioClient,
} from "@superdurable/dex-connectors-react";
import { ConfluenceSetupView } from "./setup.js";
import { ConfluenceConfigurationUnit, SitePickerUnit } from "./units.js";
import { connectionCloudID, parseAccessibleSites, parseSpacePage, type ConfluenceSite, type ConfluenceSpace } from "./provider.js";

const connectorId = "confluence";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [sites, setSites] = useState<ConfluenceSite[]>([]);
  const [spaces, setSpaces] = useState<ConfluenceSpace[]>([]);
  const [isSpaceListTruncated, setIsSpaceListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  const save = (value: Record<string, unknown>) => client.send("use.configuration.save", "use.configuration.write", {value});
  const target = ready.target;
  if (target.kind === "connection" && target.unitId === undefined) {
    return <ConfluenceSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  if (target.kind === "connection") {
    if (target.unitId !== "sitePicker") return <StudioNotice tone="error">Unsupported Confluence connection unit: {target.unitId}</StudioNotice>;
    const loadSites = async () => {
      setLoadError(undefined);
      try {
        setSites(parseAccessibleSites(await client.executeProviderCommand("listAccessibleSites", "confluence.sites-list")));
      } catch (error) {
        setLoadError(error instanceof Error ? error.message : "Confluence sites could not be loaded");
      }
    };
    return <SitePickerUnit busy={client.busy} loadError={loadError} onChooseSite={() => void loadSites()} onSave={save} sites={sites} target={target}/>;
  }
  const loadSpaces = async (cloudId: string) => {
    setLoadError(undefined);
    try {
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const parameters: Record<string, string> = {cloudId};
        if (cursor !== "") parameters.cursor = cursor;
        const page = parseSpacePage(await client.executeProviderCommand("listSpaces", "confluence.spaces-list", parameters));
        return {items: page.spaces, nextCursor: page.nextCursor};
      });
      setSpaces(items);
      setIsSpaceListTruncated(isTruncated);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Confluence spaces could not be loaded");
    }
  };
  return <ConfluenceConfigurationUnit
    busy={client.busy}
    connectionCloudId={connectionCloudID(ready.connection)}
    isSpaceListTruncated={isSpaceListTruncated}
    loadError={loadError}
    onChooseSpace={(cloudId) => void loadSpaces(cloudId)}
    onSave={save}
    spaces={spaces}
    target={target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
