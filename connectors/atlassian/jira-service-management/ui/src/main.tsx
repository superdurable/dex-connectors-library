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
import { ServiceManagementSetupView } from "./setup.js";
import { ServiceManagementConfigurationUnit, SitePickerUnit } from "./units.js";
import {
  connectionCloudID,
  parseAccessibleSites,
  parseRequestTypePage,
  parseServiceDeskPage,
  type RequestType,
  type ServiceDesk,
  type ServiceManagementSite,
} from "./provider.js";

const connectorId = "jira-service-management";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [sites, setSites] = useState<ServiceManagementSite[]>([]);
  const [serviceDesks, setServiceDesks] = useState<ServiceDesk[]>([]);
  const [requestTypes, setRequestTypes] = useState<RequestType[]>([]);
  const [isListTruncated, setIsListTruncated] = useState(false);
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
    return <ServiceManagementSetupView
      connection={ready.connection}
      onConnect={() => void client.send("oauth.connect", "oauth.connection.manage")}
      onReconnect={() => void client.send("oauth.reconnect", "oauth.connection.manage")}
    />;
  }
  if (target.kind === "connection") {
    if (target.unitId !== "sitePicker") return <StudioNotice tone="error">Unsupported Jira Service Management connection unit: {target.unitId}</StudioNotice>;
    const loadSites = async () => {
      setLoadError(undefined);
      try {
        setSites(parseAccessibleSites(await client.executeProviderCommand("listAccessibleSites", "jiraservicemanagement.sites-list")));
      } catch (error) {
        setLoadError(error instanceof Error ? error.message : "Atlassian sites could not be loaded");
      }
    };
    return <SitePickerUnit busy={client.busy} loadError={loadError} onChooseSite={() => void loadSites()} onSave={save} sites={sites} target={target}/>;
  }
  const loadServiceDesks = async (cloudId: string) => {
    setLoadError(undefined);
    try {
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const start = cursor === "" ? 0 : Number(cursor);
        const page = parseServiceDeskPage(await client.executeProviderCommand(
          "listServiceDesks", "jiraservicemanagement.service-desks-list", {cloudId, start: String(start)},
        ), start);
        return {items: page.items, nextCursor: page.nextStart};
      });
      setServiceDesks(items);
      setIsListTruncated(isTruncated);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Service desks could not be loaded");
    }
  };
  const loadRequestTypes = async (cloudId: string, serviceDeskId: string) => {
    setLoadError(undefined);
    try {
      const {items, isTruncated} = await collectProviderPages(async (cursor) => {
        const start = cursor === "" ? 0 : Number(cursor);
        const page = parseRequestTypePage(await client.executeProviderCommand(
          "listRequestTypes", "jiraservicemanagement.request-types-list", {cloudId, serviceDeskId, start: String(start)},
        ), start);
        return {items: page.items, nextCursor: page.nextStart};
      });
      setRequestTypes(items);
      setIsListTruncated(isTruncated);
    } catch (error) {
      setLoadError(error instanceof Error ? error.message : "Request types could not be loaded");
    }
  };
  return <ServiceManagementConfigurationUnit
    busy={client.busy}
    connectionCloudId={connectionCloudID(ready.connection)}
    isListTruncated={isListTruncated}
    loadError={loadError}
    onChooseRequestType={(cloudId, serviceDeskId) => void loadRequestTypes(cloudId, serviceDeskId)}
    onChooseServiceDesk={(cloudId) => void loadServiceDesks(cloudId)}
    onSave={save}
    requestTypes={requestTypes}
    serviceDesks={serviceDesks}
    target={target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
