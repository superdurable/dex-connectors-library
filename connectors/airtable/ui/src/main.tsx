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
import { AirtableSetupView } from "./setup.js";
import { AirtableConfigurationUnit } from "./units.js";
import { parseAirtableBasePage, parseAirtableTables, type AirtableBase, type AirtableTable } from "./provider.js";

const connectorId = "airtable";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [bases, setBases] = useState<AirtableBase[]>([]);
  const [isBaseListTruncated, setIsBaseListTruncated] = useState(false);
  const [tables, setTables] = useState<AirtableTable[]>([]);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  const loadBases = async () => {
    setLoadError(undefined);
    try {
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const page = parseAirtableBasePage(await client.executeProviderCommand("listBases", "airtable.bases-list", cursor ? {offset: cursor} : {}));
        return {items: page.bases, nextCursor: page.nextOffset};
      });
      setBases(items);
      setIsBaseListTruncated(isTruncated);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Airtable bases could not be loaded");
    }
  };
  const loadTables = async (baseId: string) => {
    setLoadError(undefined);
    try {
      setTables(parseAirtableTables(await client.executeProviderCommand("listTables", "airtable.tables-list", {baseId})));
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Airtable tables could not be loaded");
    }
  };

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") return <AirtableSetupView connection={ready.connection}/>;
  return <AirtableConfigurationUnit
    bases={bases}
    busy={client.busy}
    isBaseListTruncated={isBaseListTruncated}
    loadError={loadError}
    onLoadBases={() => void loadBases()}
    onLoadTables={(baseId) => void loadTables(baseId)}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    tables={tables}
    target={ready.target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
