// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  applyConnectorStudioTheme,
  observeConnectorStudioFrameAutoHeight,
  useConnectorStudioClient,
} from "@superdurable/dex-connectors-react";
import { LinearSetupView } from "./setup.js";
import { LinearConfigurationUnit, type LinearTeam } from "./units.js";
import { parseLinearTeamList } from "./provider.js";

const connectorId = "linear";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [teams, setTeams] = useState<LinearTeam[]>([]);
  const [isTeamListTruncated, setIsTeamListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  // listTeams sends one GraphQL query over GET; Linear lists at most 100 teams in it.
  const loadTeams = async () => {
    setLoadError(undefined);
    try {
      const list = parseLinearTeamList(await client.executeProviderCommand("listTeams", "linear.teams-list"));
      setTeams(list.teams);
      setIsTeamListTruncated(list.isTruncated);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Linear teams could not be loaded");
    }
  };

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  if (ready.target.kind === "connection") {
    return <LinearSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  return <LinearConfigurationUnit
    busy={client.busy}
    isTeamListTruncated={isTeamListTruncated}
    loadError={loadError}
    onLoadTeams={() => void loadTeams()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={ready.target}
    teams={teams}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
