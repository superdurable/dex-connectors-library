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
import { CalendarSetupView } from "./setup.js";
import { CalendarConfigurationUnit } from "./units.js";
import { minimumAccessRole, parseCalendarPage, type CalendarListEntry } from "./provider.js";

const connectorId = "google-calendar";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [calendars, setCalendars] = useState<CalendarListEntry[]>([]);
  const [isCalendarListTruncated, setIsCalendarListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") {
    return <CalendarSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  const target = ready.target;
  const loadCalendars = async () => {
    setLoadError(undefined);
    try {
      const minAccessRole = minimumAccessRole(target);
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const page = parseCalendarPage(await client.executeProviderCommand(
          "listCalendars", "google.calendar.calendars-list", cursor ? {minAccessRole, pageToken: cursor} : {minAccessRole},
        ));
        return {items: page.calendars, nextCursor: page.nextPageToken};
      });
      setCalendars(items);
      setIsCalendarListTruncated(isTruncated);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Calendars could not be loaded");
    }
  };
  return <CalendarConfigurationUnit
    busy={client.busy}
    calendars={calendars}
    isCalendarListTruncated={isCalendarListTruncated}
    loadError={loadError}
    onChooseCalendar={() => void loadCalendars()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
