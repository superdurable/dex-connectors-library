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
import { ZohoDeskSetupView } from "./setup.js";
import { DepartmentPickerUnit, OrganizationPickerUnit } from "./units.js";
import {
  departmentCommandForConnection, departmentsCapability, organizationCommandForConnection, organizationsCapability, parseDepartmentPage,
  parseOrganizations, type ZohoDeskDepartment, type ZohoDeskOrganization,
} from "./provider.js";

const connectorId = "zoho-desk";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [organizations, setOrganizations] = useState<ZohoDeskOrganization[]>([]);
  const [departments, setDepartments] = useState<ZohoDeskDepartment[]>([]);
  const [isDepartmentListTruncated, setIsDepartmentListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  const target = ready.target;
  if (target.kind === "connection" && target.unitId === undefined) {
    return <ZohoDeskSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  if (target.kind === "configurationUnit" && target.unitId === "departmentPicker") {
    const departmentCommand = departmentCommandForConnection(ready.connection);
    const loadDepartments = async () => {
      if (departmentCommand === undefined) return;
      setLoadError(undefined);
      try {
        const {items, isTruncated} = await collectProviderPages(async (cursor) => {
          const parameters: Record<string, string> = cursor ? {orgId: departmentCommand.orgId, from: cursor} : {orgId: departmentCommand.orgId};
          const page = parseDepartmentPage(await client.executeProviderCommand(departmentCommand.commandId, departmentsCapability, parameters), cursor ? Number(cursor) : 0);
          return {items: page.departments, nextCursor: page.nextFrom};
        }, 5);
        setDepartments(items);
        setIsDepartmentListTruncated(isTruncated);
      } catch (error) {
        setLoadError(error instanceof Error ? error.message : "Zoho Desk departments could not be loaded");
      }
    };
    return <DepartmentPickerUnit
      busy={client.busy}
      canListDepartments={departmentCommand !== undefined}
      departments={departments}
      isDepartmentListTruncated={isDepartmentListTruncated}
      loadError={loadError}
      onChooseDepartment={() => void loadDepartments()}
      onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
      target={target}
    />;
  }
  if (target.kind !== "connection" || target.unitId !== "organizationPicker") {
    return <StudioNotice tone="error">Unsupported Zoho Desk unit: {target.unitId}</StudioNotice>;
  }
  const commandId = organizationCommandForConnection(ready.connection);
  const loadOrganizations = async () => {
    if (commandId === undefined) return;
    setLoadError(undefined);
    try {
      setOrganizations(parseOrganizations(await client.executeProviderCommand(commandId, organizationsCapability)));
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Zoho Desk organizations could not be loaded");
    }
  };
  return <OrganizationPickerUnit
    busy={client.busy}
    canListOrganizations={commandId !== undefined}
    loadError={loadError}
    onChooseOrganization={() => void loadOrganizations()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    organizations={organizations}
    target={target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
