import { StrictMode, useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  connectorStudioHostAPIVersion,
  isConnectorStudioMessage,
  type ConnectorStudioCommand,
  type ConnectorStudioCommandResult,
  type ConnectorStudioHostReady,
} from "@superdurable/dex-connectors-react";
import { SpreadsheetSetupView, type SpreadsheetSelection } from "./setup.js";

const connectorId = "google-sheets";

function ConnectorApp() {
  const [ready, setReady] = useState<ConnectorStudioHostReady>();
  const [selection, setSelection] = useState<SpreadsheetSelection>({});
  const [tabs, setTabs] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const pending = useMemo(() => new Map<string, ConnectorStudioCommand["command"]>(), []);

  useEffect(() => {
    const receive = (event: MessageEvent<unknown>) => {
      if (event.source !== window.parent || !isConnectorStudioMessage(event.data)) return;
      if (event.data.type === "connector.host.ready" && event.data.connectorId === connectorId) {
        setReady(event.data);
        setSelection(event.data.configuration as SpreadsheetSelection);
        return;
      }
      if (event.data.type !== "connector.command.result" || !ready || event.data.sessionNonce !== ready.sessionNonce) return;
      const result = event.data as ConnectorStudioCommandResult;
      const command = pending.get(result.requestId);
      pending.delete(result.requestId);
      setBusy(pending.size > 0);
      if (!result.ok) return;
      if (command === "google.picker.open-spreadsheet") setSelection((current) => ({ ...current, ...result.value }));
      if (command === "google.sheets.list-tabs") setTabs(Array.isArray(result.value?.tabs) ? result.value.tabs.filter((value): value is string => typeof value === "string") : []);
    };
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, [pending, ready]);

  const send = (command: ConnectorStudioCommand["command"], input?: Record<string, unknown>) => {
    if (!ready || !ready.capabilities.includes(requiredCapability(command))) return;
    const requestId = crypto.randomUUID();
    pending.set(requestId, command); setBusy(true);
    const message: ConnectorStudioCommand = { type: "connector.command", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: ready.sessionNonce, connectorId, requestId, command, input };
    window.parent.postMessage(message, "*");
  };

  if (!ready) return <p role="status">Waiting for Studio…</p>;
  return <SpreadsheetSetupView connection={ready.connection} selection={selection} tabs={tabs} capabilities={ready.capabilities} busy={busy} onCommand={send}/>;
}

function requiredCapability(command: ConnectorStudioCommand["command"]): string {
  if (command.startsWith("oauth.")) return "oauth.connection.manage";
  if (command === "google.picker.open-spreadsheet") return "google.picker.spreadsheets";
  if (command === "google.sheets.list-tabs") return "google.sheets.tabs-list";
  return "configuration.write";
}

const style = document.createElement("style");
style.textContent = `:root{font-family:Inter,system-ui;color:#17211b;background:#f5faf6}body{margin:0}.card{padding:24px}header{display:flex;gap:12px;align-items:center}header img{width:42px;height:42px}h1{font-size:20px;margin:0}p{margin:5px 0;color:#526159}.controls{display:grid;gap:12px;margin-top:18px}label{display:grid;gap:5px;font-weight:600}input,select,button{font:inherit;padding:9px;border:1px solid #b8c8bc;border-radius:8px}button{background:#188038;color:white;border:0;cursor:pointer}.secondary{background:white;color:#8a2525;border:1px solid #d8b8b8}`;
document.head.append(style);
createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
