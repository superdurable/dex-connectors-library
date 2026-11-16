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
import { HiverSetupView } from "./setup.js";
import { HiverConfigurationUnit, type HiverInbox } from "./units.js";
import { parseHiverInboxPage } from "./provider.js";

const connectorId = "hiver";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [inboxes, setInboxes] = useState<HiverInbox[]>([]);
  const [isInboxListTruncated, setIsInboxListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  // Hiver pages inboxes with an opaque next_page token, which is the cursor.
  const loadInboxes = async () => {
    setLoadError(undefined);
    try {
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const parameters: Record<string, string> = cursor === "" ? {} : {next_page: cursor};
        const parsed = parseHiverInboxPage(await client.executeProviderCommand("listInboxes", "hiver.inboxes-list", parameters));
        return {items: parsed.inboxes, nextCursor: parsed.nextPageToken};
      });
      setInboxes(items);
      setIsInboxListTruncated(isTruncated);
    } catch {
      setLoadError("Hiver shared inboxes could not be loaded. Check that the connection's API key is saved and its app toggle is on in Hiver under Admin Panel > Integrations > Developer APIs; until then, enter the inbox ID below.");
    }
  };

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") return <HiverSetupView connection={ready.connection}/>;
  return <HiverConfigurationUnit
    busy={client.busy}
    inboxes={inboxes}
    isInboxListTruncated={isInboxListTruncated}
    loadError={loadError}
    onLoadInboxes={() => void loadInboxes()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={ready.target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
