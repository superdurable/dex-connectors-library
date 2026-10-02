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
import { OneDriveSetupView } from "./setup.js";
import { OneDriveConfigurationUnit } from "./units.js";
import {
  driveKeyFromDriveID, parseDrive, parseDrivePage, parseFolderPage, parseSitePage, siteIDParts,
  type GraphDrive, type GraphFolder, type SharePointSite,
} from "./provider.js";
import type { PickedList } from "./unit-frame.js";

const connectorId = "microsoft-onedrive";
const sitesCapability = "microsoft.sharepoint.sites-search";
const drivesCapability = "microsoft.onedrive.drives-list";
const foldersCapability = "microsoft.onedrive.folders-list";

function withSkipToken(parameters: Record<string, string>, skipToken: string): Record<string, string> {
  return skipToken ? {...parameters, skipToken} : parameters;
}

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [sites, setSites] = useState<PickedList<SharePointSite>>();
  const [drives, setDrives] = useState<PickedList<GraphDrive>>();
  const [folders, setFolders] = useState<PickedList<GraphFolder>>();
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  const load = async (loadList: () => Promise<void>, failureMessage: string) => {
    setLoadError(undefined);
    try {
      await loadList();
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : failureMessage);
    }
  };
  const searchSites = (query: string) => load(async () => {
    setSites(await collectProviderPages(async (cursor) => {
      const page = parseSitePage(await client.executeProviderCommand("searchSites", sitesCapability, withSkipToken({search: query}, cursor)));
      return {items: page.items, nextCursor: page.nextSkipToken};
    }));
  }, "SharePoint sites could not be searched");
  const listDrives = (siteId: string) => load(async () => {
    if (siteId === "") {
      setDrives({items: parseDrive(await client.executeProviderCommand("getMyDrive", drivesCapability, {})), isTruncated: false});
      return;
    }
    const parts = siteIDParts(siteId);
    if (!parts) throw new Error("The saved site ID cannot be listed; paste a drive ID instead.");
    setDrives(await collectProviderPages(async (cursor) => {
      const page = parseDrivePage(await client.executeProviderCommand("listSiteDrives", drivesCapability, withSkipToken({...parts}, cursor)));
      return {items: page.items, nextCursor: page.nextSkipToken};
    }));
  }, "Document libraries could not be listed");
  const listFolders = (driveId: string, parentFolderId: string) => load(async () => {
    const driveKey = driveKeyFromDriveID(driveId);
    setFolders(await collectProviderPages(async (cursor) => {
      const value = parentFolderId === ""
        ? await client.executeProviderCommand("listRootFolders", foldersCapability, withSkipToken({driveKey}, cursor))
        : await client.executeProviderCommand("listFolderChildren", foldersCapability, withSkipToken({driveKey, itemId: parentFolderId}, cursor));
      const page = parseFolderPage(value);
      return {items: page.items, nextCursor: page.nextSkipToken};
    }));
  }, "Folders could not be listed");

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") {
    return <OneDriveSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  return <OneDriveConfigurationUnit
    busy={client.busy}
    drives={drives}
    folders={folders}
    loadError={loadError}
    onListDrives={(siteId) => void listDrives(siteId)}
    onListFolders={(driveId, parentFolderId) => void listFolders(driveId, parentFolderId)}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    onSearchSites={(query) => void searchSites(query)}
    sites={sites}
    target={ready.target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
