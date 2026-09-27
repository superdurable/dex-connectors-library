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
import { SpreadsheetSetupView } from "./setup.js";
import { SpreadsheetConfigurationUnit } from "./units.js";
import { parseSheetTabs, parseSpreadsheetPage, type SpreadsheetFile } from "./provider.js";

const connectorId = "google-sheets";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [tabs, setTabs] = useState<string[]>([]);
  const [spreadsheets, setSpreadsheets] = useState<SpreadsheetFile[]>([]);
  const [isSpreadsheetListTruncated, setIsSpreadsheetListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  const loadSpreadsheets = async () => {
    setLoadError(undefined);
    try {
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const page = parseSpreadsheetPage(await client.executeProviderCommand("listSpreadsheets", "google.drive.spreadsheets-list", cursor ? {pageToken: cursor} : {}));
        return {items: page.files, nextCursor: page.nextPageToken};
      });
      setSpreadsheets(items);
      setIsSpreadsheetListTruncated(isTruncated);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Spreadsheets could not be loaded");
    }
  };
  const loadTabs = async (spreadsheetId: string) => {
    setLoadError(undefined);
    try {
      setTabs(parseSheetTabs(await client.executeProviderCommand("listTabs", "google.sheets.tabs-list", {spreadsheetId})));
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Sheet tabs could not be loaded");
    }
  };

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") {
    return <SpreadsheetSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  return <SpreadsheetConfigurationUnit
    busy={client.busy}
    isSpreadsheetListTruncated={isSpreadsheetListTruncated}
    loadError={loadError}
    onChooseSpreadsheet={() => void loadSpreadsheets()}
    onLoadTabs={(spreadsheetId) => void loadTabs(spreadsheetId)}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    spreadsheets={spreadsheets}
    tabs={tabs}
    target={ready.target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
