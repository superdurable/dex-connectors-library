// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  applyConnectorStudioTheme,
  observeConnectorStudioFrameAutoHeight,
  useConnectorStudioClient,
} from "@superdurable/dex-connectors-react";
import { loadIntercomAdmins, type IntercomAdmin } from "./admins.js";
import { IntercomSetupView } from "./setup.js";
import { IntercomConfigurationUnit } from "./units.js";

const connectorId = "intercom";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [admins, setAdmins] = useState<IntercomAdmin[]>([]);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  const loadAdmins = async () => {
    if (!ready) return;
    setLoadError(undefined);
    try {
      setAdmins(await loadIntercomAdmins(client, ready.connection));
    } catch (error) {
      setLoadError(error instanceof Error ? `Intercom admins could not be loaded: ${error.message}` : "Intercom admins could not be loaded");
    }
  };

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") return <IntercomSetupView connection={ready.connection}/>;
  return <IntercomConfigurationUnit
    admins={admins}
    busy={client.busy}
    loadError={loadError}
    onLoadAdmins={() => void loadAdmins()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={ready.target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
