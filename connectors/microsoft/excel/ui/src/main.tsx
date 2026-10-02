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
import { ExcelSetupView } from "./setup.js";
import { ExcelConfigurationUnit } from "./units.js";
import {
  driveKeyFromDriveId,
  encodeSharingUrl,
  parseSharedWorkbook,
  parseTables,
  parseWorkbookSearchPage,
  parseWorksheets,
  type ExcelTable,
  type ExcelWorkbook,
  type ExcelWorksheet,
} from "./provider.js";

const connectorId = "microsoft-excel";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [workbooks, setWorkbooks] = useState<ExcelWorkbook[]>([]);
  const [isWorkbookListTruncated, setIsWorkbookListTruncated] = useState(false);
  const [resolvedWorkbook, setResolvedWorkbook] = useState<ExcelWorkbook>();
  const [worksheets, setWorksheets] = useState<ExcelWorksheet[]>([]);
  const [tables, setTables] = useState<ExcelTable[]>([]);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  const load = async (action: () => Promise<void>, fallback: string) => {
    setLoadError(undefined);
    try {
      await action();
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : fallback);
    }
  };
  const searchWorkbooks = () => load(async () => {
    const {items, isTruncated} = await collectProviderPages(async (cursor) => {
      const page = parseWorkbookSearchPage(await client.executeProviderCommand("searchWorkbooks", "microsoft.excel.workbooks-search", cursor ? {skipToken: cursor} : {}));
      return {items: page.workbooks, nextCursor: page.nextSkipToken};
    }, 5);
    setWorkbooks(items);
    setIsWorkbookListTruncated(isTruncated);
  }, "Workbooks could not be loaded");
  const resolveSharingLink = (link: string) => load(async () => {
    const encodedSharingUrl = encodeSharingUrl(link);
    setResolvedWorkbook(parseSharedWorkbook(await client.executeProviderCommand("resolveSharedWorkbook", "microsoft.excel.shared-workbook-resolve", {encodedSharingUrl})));
  }, "The sharing link could not be opened");
  const loadWorksheets = (driveId: string, workbookId: string) => load(async () => {
    setWorksheets(parseWorksheets(await client.executeProviderCommand("listWorksheets", "microsoft.excel.worksheets-list", {driveKey: driveKeyFromDriveId(driveId), workbookId})));
  }, "Worksheets could not be loaded");
  const loadTables = (driveId: string, workbookId: string) => load(async () => {
    setTables(parseTables(await client.executeProviderCommand("listTables", "microsoft.excel.tables-list", {driveKey: driveKeyFromDriveId(driveId), workbookId})));
  }, "Tables could not be loaded");

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") {
    return <ExcelSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  return <ExcelConfigurationUnit
    busy={client.busy}
    isWorkbookListTruncated={isWorkbookListTruncated}
    loadError={loadError}
    onLoadTables={(driveId, workbookId) => void loadTables(driveId, workbookId)}
    onLoadWorksheets={(driveId, workbookId) => void loadWorksheets(driveId, workbookId)}
    onResolveSharingLink={(link) => void resolveSharingLink(link)}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    onSearchWorkbooks={() => void searchWorkbooks()}
    resolvedWorkbook={resolvedWorkbook}
    tables={tables}
    target={ready.target}
    workbooks={workbooks}
    worksheets={worksheets}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
