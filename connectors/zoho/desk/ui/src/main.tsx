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
import { ZohoDeskSetupView } from "./setup.js";
import { OrganizationPickerUnit } from "./units.js";
import { organizationCommandForConnection, organizationsCapability, parseOrganizations, type ZohoDeskOrganization } from "./provider.js";

const connectorId = "zoho-desk";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [organizations, setOrganizations] = useState<ZohoDeskOrganization[]>([]);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  const target = ready.target;
  if (target.kind === "connection" && target.unitId === undefined) {
    return <ZohoDeskSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  if (target.kind !== "connection" || target.unitId !== "organizationPicker") {
    return <StudioNotice tone="error">Unsupported Zoho Desk unit: {target.unitId}</StudioNotice>;
  }
  const commandId = organizationCommandForConnection(ready.connection);
  const loadOrganizations = async () => {
    if (commandId === undefined) return;
    setLoadError(undefined);
    try {
      setOrganizations(parseOrganizations(await client.executeProviderCommand(commandId, organizationsCapability)));
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Zoho Desk organizations could not be loaded");
    }
  };
  return <OrganizationPickerUnit
    busy={client.busy}
    canListOrganizations={commandId !== undefined}
    loadError={loadError}
    onChooseOrganization={() => void loadOrganizations()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    organizations={organizations}
    target={target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
