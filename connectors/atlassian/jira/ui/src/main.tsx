// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  StudioNotice,
  applyConnectorStudioTheme,
  collectProviderPages,
  observeConnectorStudioFrameAutoHeight,
  useConnectorStudioClient,
} from "@superdurable/dex-connectors-react";
import { JiraSetupView } from "./setup.js";
import { JiraConfigurationUnit, SitePickerUnit } from "./units.js";
import { connectionCloudID, parseAccessibleSites, parseProjectPage, projectAction, type JiraProject, type JiraSite } from "./provider.js";

const connectorId = "jira";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [sites, setSites] = useState<JiraSite[]>([]);
  const [projects, setProjects] = useState<JiraProject[]>([]);
  const [isProjectListTruncated, setIsProjectListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  const save = (value: Record<string, unknown>) => client.send("use.configuration.save", "use.configuration.write", {value});
  const target = ready.target;
  if (target.kind === "connection" && target.unitId === undefined) {
    return <JiraSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  if (target.kind === "connection") {
    if (target.unitId !== "sitePicker") return <StudioNotice tone="error">Unsupported Jira connection unit: {target.unitId}</StudioNotice>;
    const loadSites = async () => {
      setLoadError(undefined);
      try {
        setSites(parseAccessibleSites(await client.executeProviderCommand("listAccessibleSites", "jira.sites-list")));
      } catch (error) {
        setLoadError(error instanceof Error ? error.message : "Jira sites could not be loaded");
      }
    };
    return <SitePickerUnit busy={client.busy} loadError={loadError} onChooseSite={() => void loadSites()} onSave={save} sites={sites} target={target}/>;
  }
  const loadProjects = async (cloudId: string) => {
    setLoadError(undefined);
    try {
      const action = projectAction(target);
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const startAt = cursor === "" ? 0 : Number(cursor);
        const page = parseProjectPage(await client.executeProviderCommand(
          "listProjects", "jira.projects-list", {cloudId, action, startAt: String(startAt)},
        ), startAt);
        return {items: page.projects, nextCursor: page.nextStartAt};
      });
      setProjects(items);
      setIsProjectListTruncated(isTruncated);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Jira projects could not be loaded");
    }
  };
  return <JiraConfigurationUnit
    busy={client.busy}
    connectionCloudId={connectionCloudID(ready.connection)}
    isProjectListTruncated={isProjectListTruncated}
    loadError={loadError}
    onChooseProject={(cloudId) => void loadProjects(cloudId)}
    onSave={save}
    projects={projects}
    target={target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
