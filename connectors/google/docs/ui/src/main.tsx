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
import { DocsSetupView } from "./setup.js";
import { DocsConfigurationUnit } from "./units.js";
import { parseDocumentPage, parseFolderPage, type DocsDocument, type DriveFolder } from "./provider.js";

const connectorId = "google-docs";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [documents, setDocuments] = useState<DocsDocument[]>([]);
  const [isDocumentListTruncated, setIsDocumentListTruncated] = useState(false);
  const [folders, setFolders] = useState<DriveFolder[]>([]);
  const [isFolderListTruncated, setIsFolderListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  const loadDocuments = async () => {
    setLoadError(undefined);
    try {
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const page = parseDocumentPage(await client.executeProviderCommand("listDocuments", "google.docs.documents-list", cursor ? {pageToken: cursor} : {}));
        return {items: page.documents, nextCursor: page.nextPageToken};
      });
      setDocuments(items);
      setIsDocumentListTruncated(isTruncated);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Documents could not be loaded");
    }
  };

  const loadFolders = async () => {
    setLoadError(undefined);
    try {
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const page = parseFolderPage(await client.executeProviderCommand("listFolders", "google.docs.folders-list", cursor ? {pageToken: cursor} : {}));
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
    return <DocsSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  return <DocsConfigurationUnit
    busy={client.busy}
    documents={documents}
    folders={folders}
    isDocumentListTruncated={isDocumentListTruncated}
    isFolderListTruncated={isFolderListTruncated}
    loadError={loadError}
    onChooseDocument={() => void loadDocuments()}
    onChooseFolder={() => void loadFolders()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={ready.target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
