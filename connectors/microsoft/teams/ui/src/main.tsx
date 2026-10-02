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
import { TeamsSetupView } from "./setup.js";
import { TeamsConfigurationUnit } from "./units.js";
import { parseChannelPage, parseChatPage, parseJoinedTeams, type TeamsChannel, type TeamsChat, type TeamsTeam } from "./provider.js";

const connectorId = "microsoft-teams";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [teams, setTeams] = useState<TeamsTeam[]>([]);
  const [channels, setChannels] = useState<TeamsChannel[]>([]);
  const [chats, setChats] = useState<TeamsChat[]>([]);
  const [isListTruncated, setIsListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  const load = async (loadList: () => Promise<boolean>, failure: string) => {
    setLoadError(undefined);
    try {
      setIsListTruncated(await loadList());
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : failure);
    }
  };
  const loadTeams = () => load(async () => {
    setTeams(parseJoinedTeams(await client.executeProviderCommand("listJoinedTeams", "teams.joined-teams-list")));
    return false;
  }, "Microsoft Teams teams could not be loaded");
  const loadChannels = (teamId: string) => load(async () => {
    const {items, isTruncated} = await collectProviderPages(async (skipToken) => {
      const parameters: Record<string, string> = skipToken ? {teamId, skipToken} : {teamId};
      const page = parseChannelPage(await client.executeProviderCommand("listChannels", "teams.channels-list", parameters));
      return {items: page.items, nextCursor: page.nextSkipToken};
    });
    setChannels(items);
    return isTruncated;
  }, "Microsoft Teams channels could not be loaded");
  const loadChats = () => load(async () => {
    const {items, isTruncated} = await collectProviderPages(async (skipToken) => {
      const page = parseChatPage(await client.executeProviderCommand("listChats", "teams.chats-list", skipToken ? {skipToken} : {}));
      return {items: page.items, nextCursor: page.nextSkipToken};
    });
    setChats(items);
    return isTruncated;
  }, "Microsoft Teams chats could not be loaded");

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") {
    return <TeamsSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  return <TeamsConfigurationUnit
    busy={client.busy}
    channels={channels}
    chats={chats}
    isListTruncated={isListTruncated}
    loadError={loadError}
    onLoadChannels={(teamId) => void loadChannels(teamId)}
    onLoadChats={() => void loadChats()}
    onLoadTeams={() => void loadTeams()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={ready.target}
    teams={teams}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
