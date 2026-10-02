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
import { OutlookMailSetupView } from "./setup.js";
import { OutlookMailConfigurationUnit } from "./units.js";
import { mailFolderCommands, mailFoldersCapability, mergeFolders, parseMailFolderPage, type MailFolder, type ProviderCommandRequest } from "./provider.js";

const connectorId = "outlook-mail";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [folders, setFolders] = useState<MailFolder[]>([]);
  const [isFolderListTruncated, setIsFolderListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") {
    return <OutlookMailSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  const commands = mailFolderCommands(ready.connection);
  // Graph pages folders with $skip; each page lists one parent's direct children only.
  const loadFolderPages = async (request: (skip: string) => ProviderCommandRequest, parentPath: string) => {
    setLoadError(undefined);
    try {
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const {commandId, parameters} = request(cursor);
        const page = parseMailFolderPage(await client.executeProviderCommand(commandId, mailFoldersCapability, parameters), parentPath, cursor === "" ? 0 : Number(cursor));
        return {items: page.folders, nextCursor: page.nextSkip};
      });
      setFolders((current) => mergeFolders(current, items));
      setIsFolderListTruncated((current) => current || isTruncated);
    } catch {
      setLoadError("Mail folders could not be loaded. Authorize the connection, or for an app-only connection run the application once so it stores an access token; until then, enter a folder ID or a well-known name below.");
    }
  };
  return <OutlookMailConfigurationUnit
    busy={client.busy}
    folders={folders}
    isFolderListTruncated={isFolderListTruncated}
    loadError={commands.isAvailable ? loadError : commands.reason}
    onLoadChildFolders={(folder) => { if (commands.isAvailable) void loadFolderPages((skip) => commands.children(folder.id, skip), folder.path); }}
    onLoadFolders={() => { if (commands.isAvailable) void loadFolderPages(commands.topLevel, ""); }}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={ready.target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
