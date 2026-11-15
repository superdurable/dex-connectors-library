// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StrictMode, useEffect } from "react";
import { createRoot } from "react-dom/client";
import {
  applyConnectorStudioTheme,
  observeConnectorStudioFrameAutoHeight,
  useConnectorStudioClient,
} from "@superdurable/dex-connectors-react";
import { MailchimpSetupView } from "./setup.js";
import { MailchimpConfigurationUnit } from "./units.js";

const connectorId = "mailchimp";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") return <MailchimpSetupView connection={ready.connection}/>;
  return <MailchimpConfigurationUnit
    busy={client.busy}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={ready.target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
