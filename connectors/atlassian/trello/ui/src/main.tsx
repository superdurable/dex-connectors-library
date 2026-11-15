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
import { TrelloSetupView } from "./setup.js";
import { TrelloConfigurationUnit } from "./units.js";

const connectorId = "trello";

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
    if (target.unitId !== undefined) return <StudioNotice tone="error">Unsupported Trello connection unit: {target.unitId}</StudioNotice>;
    return <TrelloSetupView connection={ready.connection}/>;
  }
  return <TrelloConfigurationUnit
    busy={client.busy}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
