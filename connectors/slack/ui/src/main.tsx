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
import { SlackSetupView } from "./setup.js";
import { SlackConfigurationUnit, type SlackChannel, type SlackUser } from "./units.js";
import { parseSlackChannelPage, parseSlackUserPage } from "./provider.js";

const connectorId = "slack";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [channels, setChannels] = useState<SlackChannel[]>([]);
  const [users, setUsers] = useState<SlackUser[]>([]);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  const loadChannels = async () => {
    setLoadError(undefined);
    try {
      const {items} = await collectProviderPages(async (cursor) => {
        const page = parseSlackChannelPage(await client.executeProviderCommand("listChannels", "slack.channels-list", cursor ? {cursor} : {}));
        return {items: page.channels, nextCursor: page.nextCursor};
      });
      setChannels(items);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Slack channels could not be loaded");
    }
  };
  const loadUsers = async () => {
    setLoadError(undefined);
    try {
      const {items} = await collectProviderPages(async (cursor) => {
        const page = parseSlackUserPage(await client.executeProviderCommand("listUsers", "slack.users-list", cursor ? {cursor} : {}));
        return {items: page.users, nextCursor: page.nextCursor};
      });
      setUsers(items);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Slack members could not be loaded");
    }
  };

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") {
    return <SlackSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  return <SlackConfigurationUnit
    busy={client.busy}
    channels={channels}
    loadError={loadError}
    onLoadChannels={() => void loadChannels()}
    onLoadUsers={() => void loadUsers()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={ready.target}
    users={users}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
