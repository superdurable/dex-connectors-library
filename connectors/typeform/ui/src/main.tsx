// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  StudioNotice,
  applyConnectorStudioTheme,
  observeConnectorStudioFrameAutoHeight,
  useConnectorStudioClient,
} from "@superdurable/dex-connectors-react";
import { TypeformSetupView } from "./setup.js";
import { TypeformConfigurationUnit } from "./units.js";
import { formListCapability, listTypeformForms, type TypeformForm } from "./provider.js";

const connectorId = "typeform";

function ConnectorApp() {
  const client = useConnectorStudioClient(connectorId);
  const {ready} = client;
  const [forms, setForms] = useState<TypeformForm[]>([]);
  const [isFormListTruncated, setIsFormListTruncated] = useState(false);
  const [loadError, setLoadError] = useState<string>();
  useEffect(() => {
    if (!ready) return undefined;
    applyConnectorStudioTheme(ready);
    return observeConnectorStudioFrameAutoHeight(ready);
  }, [ready]);

  const loadForms = async () => {
    setLoadError(undefined);
    try {
      const list = await listTypeformForms((commandId, parameters) => client.executeProviderCommand(commandId, formListCapability, parameters));
      setForms(list.forms);
      setIsFormListTruncated(list.isTruncated);
    } catch (error) {
      setLoadError(`Typeform forms could not be loaded: ${error instanceof Error ? error.message : "unknown error"}. Enter the form ID instead.`);
    }
  };

  if (!ready) return <p className="studio-muted" role="status">Waiting for Dex Web…</p>;
  const target = ready.target;
  if (target.kind === "connection") {
    if (target.unitId !== undefined) return <StudioNotice tone="error">Unsupported Typeform connection unit: {target.unitId}</StudioNotice>;
    return <TypeformSetupView connection={ready.connection}/>;
  }
  return <TypeformConfigurationUnit
    busy={client.busy}
    forms={forms}
    isFormListTruncated={isFormListTruncated}
    loadError={loadError}
    onLoadForms={() => void loadForms()}
    onSave={(value) => client.send("use.configuration.save", "use.configuration.write", {value})}
    target={target}
  />;
}

createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
