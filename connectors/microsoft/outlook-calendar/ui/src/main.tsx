// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  applyConnectorStudioTheme,
  observeConnectorStudioFrameAutoHeight,
  useConnectorStudioClient,
} from "@superdurable/dex-connectors-react";
import { CalendarSetupView } from "./setup.js";
import { CalendarConfigurationUnit } from "./units.js";
import {
  calendarsListCapability,
  parseCalendarPage,
  selectCalendarListCommand,
  shouldListOnlyEditableCalendars,
  type CalendarListEntry,
} from "./provider.js";

const connectorId = "outlook-calendar";

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
      authMethodIds={ready.connection.authMethodIds}
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  const target = ready.target;
  const loadCalendars = async () => {
    setLoadError(undefined);
    const command = selectCalendarListCommand(ready.connection);
    if ("error" in command) {
      setLoadError(command.error);
      return;
    }
    try {
      const page = parseCalendarPage(await client.executeProviderCommand(command.commandId, calendarsListCapability, command.parameters));
      const isEditableOnly = shouldListOnlyEditableCalendars(target);
      setCalendars(page.calendars.filter((calendar) => !isEditableOnly || calendar.canEdit));
      setIsCalendarListTruncated(page.isTruncated);
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
