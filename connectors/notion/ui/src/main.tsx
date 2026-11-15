// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  StudioNotice,
  applyConnectorStudioTheme,
  observeConnectorStudioFrameAutoHeight,
  useConnectorStudioClient,
} from "@superdurable/dex-connectors-react";
import { NotionSetupView } from "./setup.js";
import { NotionConfigurationUnit } from "./units.js";
import { dataSourceReadCapability, listDatabaseDataSources, readDataSource, type NotionDataSource } from "./provider.js";

const connectorId = "notion";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [dataSources, setDataSources] = useState<NotionDataSource[]>([]);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  const executeCommand = (commandId: string, parameters: Record<string, string>) =>
    client.executeProviderCommand(commandId, dataSourceReadCapability, parameters);
  const loadDataSources = async (databaseLinkOrID: string) => {
    setLoadError(undefined);
    try {
      const listed = await listDatabaseDataSources(executeCommand, databaseLinkOrID);
      setDataSources(listed);
      if (listed.length === 0) setLoadError("The database lists no data source this connection can read; paste a data source ID instead.");
    } catch (error) {
      setLoadError(`Notion data sources could not be loaded: ${error instanceof Error ? error.message : "unknown error"}.`);
    }
  };
  const checkDataSource = async (dataSourceID: string) => {
    setLoadError(undefined);
    try {
      const checked = await readDataSource(executeCommand, dataSourceID);
      setDataSources((current) => [...current.filter((dataSource) => dataSource.id !== checked.id), checked]);
    } catch (error) {
      setLoadError(`Notion could not read the data source: ${error instanceof Error ? error.message : "unknown error"}.`);
    }
  };

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  const target = ready.target;
  if (target.kind === "connection") {
    if (target.unitId !== undefined) return <StudioNotice tone="error">Unsupported Notion connection unit: {target.unitId}</StudioNotice>;
    return <NotionSetupView connection={ready.connection}/>;
  }
  return <NotionConfigurationUnit
    busy={client.busy}
    dataSources={dataSources}
    loadError={loadError}
    onCheckDataSource={(dataSourceID) => void checkDataSource(dataSourceID)}
    onLoadDataSources={(databaseLinkOrID) => void loadDataSources(databaseLinkOrID)}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
