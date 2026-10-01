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
import { HelpScoutSetupView } from "./setup.js";
import { HelpScoutConfigurationUnit, type HelpScoutMailbox } from "./units.js";
import { parseHelpScoutMailboxPage } from "./provider.js";

const connectorId = "helpscout";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [mailboxes, setMailboxes] = useState<HelpScoutMailbox[]>([]);
  const [isMailboxListTruncated, setIsMailboxListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  // Help Scout pages inboxes 50 at a time; the page number is the cursor.
  const loadMailboxes = async () => {
    setLoadError(undefined);
    try {
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const page = cursor === "" ? 1 : Number(cursor);
        const parsed = parseHelpScoutMailboxPage(await client.executeProviderCommand("listMailboxes", "helpscout.mailboxes-list", {page: String(page)}), page);
        return {items: parsed.mailboxes, nextCursor: parsed.nextPage};
      });
      setMailboxes(items);
      setIsMailboxListTruncated(isTruncated);
    } catch {
      // Until the application first calls Help Scout, the connection stores no access token to list with.
      setLoadError("Help Scout inboxes could not be loaded. The list needs the access token the application stores on its first Help Scout call; until then, enter the inbox ID below.");
    }
  };

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") return <HelpScoutSetupView connection={ready.connection}/>;
  return <HelpScoutConfigurationUnit
    busy={client.busy}
    isMailboxListTruncated={isMailboxListTruncated}
    loadError={loadError}
    mailboxes={mailboxes}
    onLoadMailboxes={() => void loadMailboxes()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={ready.target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
