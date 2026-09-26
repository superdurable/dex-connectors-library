import { StrictMode, useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { connectorStudioHostAPIVersion, isConnectorStudioMessage, observeConnectorStudioFrameAutoHeight, type ConnectorStudioCommand, type ConnectorStudioCommandResult, type ConnectorStudioHostReady } from "@superdurable/dex-connectors-react";
import { SpreadsheetSetupView } from "./setup.js";
import { SpreadsheetConfigurationUnit } from "./units.js";

const connectorId = "google-sheets";

function ConnectorApp() {
  const [ready, setReady] = useState<ConnectorStudioHostReady>();
  const [tabs, setTabs] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const pending = useMemo(() => new Map<string, ConnectorStudioCommand["command"]>(), []);
  useEffect(() => ready ? observeConnectorStudioFrameAutoHeight(ready) : undefined, [ready]);
  useEffect(() => {
    const receive = (event: MessageEvent<unknown>) => {
      if (event.source !== window.parent || !isConnectorStudioMessage(event.data) || event.data.connectorId !== connectorId) return;
      if (event.data.type === "connector.host.ready") { setReady(event.data); return; }
      if (event.data.type !== "connector.command.result" || !ready || event.data.sessionNonce !== ready.sessionNonce) return;
      const result = event.data as ConnectorStudioCommandResult;
      const command = pending.get(result.requestId); pending.delete(result.requestId); setBusy(pending.size > 0);
      if (!result.ok) return;
      if (command === "google.sheets.list-tabs") setTabs(Array.isArray(result.value?.tabs) ? result.value.tabs.filter((value): value is string => typeof value === "string") : []);
      if (command === "google.picker.open-spreadsheet" && ready.target.kind === "configurationUnit") send("use.configuration.save", {value: result.value ?? {}});
    };
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, [pending, ready]);
  const send = (command: ConnectorStudioCommand["command"], input?: Record<string, unknown>) => {
    if (!ready || !ready.capabilities.includes(requiredCapability(command))) return;
    const requestId = crypto.randomUUID(); pending.set(requestId, command); setBusy(true);
    window.parent.postMessage({type: "connector.command", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: ready.sessionNonce, connectorId, requestId, command, input} satisfies ConnectorStudioCommand, "*");
  };
  if (!ready) return <p role="status">Waiting for Studio…</p>;
  if (ready.target.kind === "connection") return <SpreadsheetSetupView connection={ready.connection} onConnect={() => send("oauth.connect")} onReconnect={() => send("oauth.reconnect")}/>;
  return <SpreadsheetConfigurationUnit target={ready.target} tabs={tabs} busy={busy} onChooseSpreadsheet={() => send("google.picker.open-spreadsheet")} onLoadTabs={(spreadsheetId) => send("google.sheets.list-tabs", {spreadsheetId})} onSave={(value) => send("use.configuration.save", {value})}/>;
}

function requiredCapability(command: ConnectorStudioCommand["command"]): string {
  if (command.startsWith("oauth.")) return "oauth.connection.manage";
  if (command === "google.picker.open-spreadsheet") return "google.picker.spreadsheets";
  if (command === "google.sheets.list-tabs") return "google.sheets.tabs-list";
  return "use.configuration.write";
}

const style = document.createElement("style");
style.textContent = `:root{font-family:Inter,system-ui;color:#17211b;background:#f5faf6}body{margin:0}.card,.unit{padding:24px}header{display:flex;gap:12px;align-items:center}header img{width:42px;height:42px}h1,h2{font-size:20px;margin:0}p{margin:5px 0;color:#526159}.unit{display:grid;gap:12px}label{display:grid;gap:5px;font-weight:600}input,select,button{font:inherit;padding:9px;border:1px solid #b8c8bc;border-radius:8px}button{background:#188038;color:white;border:0;cursor:pointer}`;
document.head.append(style);
createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
