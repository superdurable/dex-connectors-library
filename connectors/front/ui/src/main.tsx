// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  applyConnectorStudioTheme,
  observeConnectorStudioFrameAutoHeight,
  useConnectorStudioClient,
} from "@superdurable/dex-connectors-react";
import { loadFrontResources, type FrontResource, type FrontResourcePicker } from "./resources.js";
import { FrontSetupView } from "./setup.js";
import { FrontConfigurationUnit } from "./units.js";

const connectorId = "front";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [resources, setResources] = useState<FrontResource[]>([]);
  const [isListTruncated, setIsListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  const loadResources = async (picker: FrontResourcePicker) => {
    setLoadError(undefined);
    try {
      const listed = await loadFrontResources(client, picker);
      setResources(listed.resources);
      setIsListTruncated(listed.isTruncated);
    } catch {
      setLoadError(`Front ${picker.pluralNoun} could not be loaded. Check that the connection's API token can read ${picker.pluralNoun}, or enter the ID below.`);
    }
  };

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") return <FrontSetupView connection={ready.connection}/>;
  return <FrontConfigurationUnit
    busy={client.busy}
    isListTruncated={isListTruncated}
    loadError={loadError}
    onLoadResources={(picker) => void loadResources(picker)}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    resources={resources}
    target={ready.target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
