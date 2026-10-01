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
import { FormsSetupView } from "./setup.js";
import { FormsConfigurationUnit } from "./units.js";
import { parseFormPage, type GoogleForm } from "./provider.js";

const connectorId = "google-forms";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [forms, setForms] = useState<GoogleForm[]>([]);
  const [isFormListTruncated, setIsFormListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  const loadForms = async () => {
    setLoadError(undefined);
    try {
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const page = parseFormPage(await client.executeProviderCommand("listForms", "google.forms.forms-list", cursor ? {pageToken: cursor} : {}));
        return {items: page.forms, nextCursor: page.nextPageToken};
      });
      setForms(items);
      setIsFormListTruncated(isTruncated);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Forms could not be loaded");
    }
  };

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") {
    return <FormsSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  return <FormsConfigurationUnit
    busy={client.busy}
    forms={forms}
    isFormListTruncated={isFormListTruncated}
    loadError={loadError}
    onChooseForm={() => void loadForms()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={ready.target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
