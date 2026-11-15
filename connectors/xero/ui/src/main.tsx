// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StrictMode, useEffect } from "react";
import { createRoot } from "react-dom/client";
import {
  StudioNotice,
  applyConnectorStudioTheme,
  observeConnectorStudioFrameAutoHeight,
  useConnectorStudioClient,
} from "@superdurable/dex-connectors-react";
import { XeroSetupView } from "./setup.js";
import { XeroConfigurationUnit } from "./units.js";

const connectorId = "xero";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  const target = ready.target;
  if (target.kind === "connection") {
    if (target.unitId !== undefined) return <StudioNotice tone="error">Unsupported Xero connection unit: {target.unitId}</StudioNotice>;
    return <XeroSetupView connection={ready.connection}/>;
  }
  return <XeroConfigurationUnit
    busy={client.busy}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
