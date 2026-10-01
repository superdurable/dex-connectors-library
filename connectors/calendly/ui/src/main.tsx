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
import { CalendlySetupView } from "./setup.js";
import { CalendlyConfigurationUnit, type CalendlyEventType } from "./units.js";
import { parseCalendlyCurrentUserURI, parseCalendlyEventTypePage } from "./provider.js";

const connectorId = "calendly";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [eventTypes, setEventTypes] = useState<CalendlyEventType[]>([]);
  const [isEventTypeListTruncated, setIsEventTypeListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  // Calendly lists event types per user, so the picker first reads the connected user's URI.
  const loadEventTypes = async () => {
    setLoadError(undefined);
    try {
      const user = parseCalendlyCurrentUserURI(await client.executeProviderCommand("getCurrentUser", "calendly.current-user-read"));
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const page = parseCalendlyEventTypePage(await client.executeProviderCommand("listEventTypes", "calendly.event-types-list",
          cursor ? {user, pageToken: cursor} : {user}));
        return {items: page.eventTypes, nextCursor: page.nextPageToken};
      });
      setEventTypes(items);
      setIsEventTypeListTruncated(isTruncated);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Calendly event types could not be loaded");
    }
  };

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") {
    return <CalendlySetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  return <CalendlyConfigurationUnit
    busy={client.busy}
    eventTypes={eventTypes}
    isEventTypeListTruncated={isEventTypeListTruncated}
    loadError={loadError}
    onLoadEventTypes={() => void loadEventTypes()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={ready.target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
