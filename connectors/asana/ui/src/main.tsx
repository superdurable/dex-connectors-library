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
  type ConnectorStudioClient,
} from "@superdurable/dex-connectors-react";
import { AsanaSetupView } from "./setup.js";
import { AsanaConfigurationUnit, type AsanaResourceList } from "./units.js";
import { offsetParameters, parseResourcePage } from "./provider.js";

const connectorId = "asana";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [workspaces, setWorkspaces] = useState<AsanaResourceList>();
  const [projects, setProjects] = useState<AsanaResourceList>();
  const [sections, setSections] = useState<AsanaResourceList>();
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  const target = ready.target;
  if (target.kind === "connection") {
    if (target.unitId !== undefined) return <StudioNotice tone="error">Unsupported Asana connection unit: {target.unitId}</StudioNotice>;
    return <AsanaSetupView connection={ready.connection}/>;
  }
  const load = async (subject: string, commandId: string, capability: string, parameters: Record<string, string>, store: (list: AsanaResourceList) => void) => {
    setLoadError(undefined);
    try {
      store(await listResources(client, commandId, capability, parameters));
    } catch (error) {
      setLoadError(`Asana ${subject} could not be loaded: ${error instanceof Error ? error.message : "unknown error"}. Enter the gid instead.`);
    }
  };
  return <AsanaConfigurationUnit
    busy={client.busy}
    loadError={loadError}
    onLoadProjects={(workspaceId) => void load("projects", "listProjects", "asana.projects-list", {workspace: workspaceId}, (list) => { setProjects(list); setSections(undefined); })}
    onLoadSections={(projectId) => void load("sections", "listSections", "asana.sections-list", {projectId}, setSections)}
    onLoadWorkspaces={() => void load("workspaces", "listWorkspaces", "asana.workspaces-list", {}, setWorkspaces)}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    projects={projects}
    sections={sections}
    target={target}
    workspaces={workspaces}
  />;
}

/** listResources follows Asana's offset through at most 20 pages of 100 entries. */
async function listResources(client: ConnectorStudioClient, commandId: string, capability: string, parameters: Record<string, string>): Promise<AsanaResourceList> {
  const {items, isTruncated} = await collectProviderPages(async (offset) => {
    const page = parseResourcePage(await client.executeProviderCommand(commandId, capability, offsetParameters(offset, parameters)));
    return {items: page.resources, nextCursor: page.nextOffset};
  });
  return {resources: items, isTruncated};
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
