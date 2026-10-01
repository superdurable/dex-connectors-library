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
import { DriveSetupView } from "./setup.js";
import { DriveConfigurationUnit } from "./units.js";
import { parseFolderPage, type DriveFolder } from "./provider.js";

const connectorId = "google-drive";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [folders, setFolders] = useState<DriveFolder[]>([]);
  const [isFolderListTruncated, setIsFolderListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  const loadFolders = async () => {
    setLoadError(undefined);
    try {
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const page = parseFolderPage(await client.executeProviderCommand("listFolders", "google.drive.folders-list", cursor ? {pageToken: cursor} : {}));
        return {items: page.folders, nextCursor: page.nextPageToken};
      });
      setFolders(items);
      setIsFolderListTruncated(isTruncated);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Folders could not be loaded");
    }
  };

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") {
    return <DriveSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  return <DriveConfigurationUnit
    busy={client.busy}
    folders={folders}
    isFolderListTruncated={isFolderListTruncated}
    loadError={loadError}
    onChooseFolder={() => void loadFolders()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={ready.target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
