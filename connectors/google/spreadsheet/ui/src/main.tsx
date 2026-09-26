import { StrictMode, useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import { connectorStudioHostAPIVersion, isConnectorStudioMessage, observeConnectorStudioFrameAutoHeight, type ConnectorStudioCommand, type ConnectorStudioCommandResult, type ConnectorStudioHostReady } from "@superdurable/dex-connectors-react";
import { SpreadsheetSetupView } from "./setup.js";
import { SpreadsheetConfigurationUnit } from "./units.js";
import { parseSheetTabs, parseSpreadsheetPage, type SpreadsheetFile } from "./provider.js";

const connectorId = "google-sheets";

function ConnectorApp() {
  const [ready, setReady] = useState<ConnectorStudioHostReady>();
  const [tabs, setTabs] = useState<string[]>([]);
  const [spreadsheets, setSpreadsheets] = useState<SpreadsheetFile[]>([]);
  const [busy, setBusy] = useState(false);
  const pending = useMemo(() => new Map<string, {resolve(value: Record<string, unknown>): void; reject(error: Error): void}>(), []);
  useEffect(() => ready ? observeConnectorStudioFrameAutoHeight(ready) : undefined, [ready]);
  useEffect(() => {
    const receive = (event: MessageEvent<unknown>) => {
      if (event.source !== window.parent || !isConnectorStudioMessage(event.data) || event.data.connectorId !== connectorId) return;
      if (event.data.type === "connector.host.ready") { setReady(event.data); return; }
      if (event.data.type !== "connector.command.result" || !ready || event.data.sessionNonce !== ready.sessionNonce) return;
      const result = event.data as ConnectorStudioCommandResult;
      const request = pending.get(result.requestId); pending.delete(result.requestId); setBusy(pending.size > 0);
      if (!request) return;
      if (result.ok) request.resolve(result.value ?? {});
      else request.reject(new Error(result.error?.message ?? "Connector command failed"));
    };
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, [pending, ready]);
  const send = (command: ConnectorStudioCommand["command"], capability: string, input?: Record<string, unknown>) => new Promise<Record<string, unknown>>((resolve, reject) => {
    if (!ready || !ready.capabilities.includes(capability)) { reject(new Error(`Missing capability ${capability}`)); return; }
    const requestId = crypto.randomUUID(); pending.set(requestId, {resolve, reject}); setBusy(true);
    window.parent.postMessage({type: "connector.command", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: ready.sessionNonce, connectorId, requestId, command, input} satisfies ConnectorStudioCommand, "*");
  });
  const executeProvider = (commandId: string, capability: string, parameters: Record<string, string> = {}) => send("provider.command.execute", capability, {commandId, parameters});
  const loadSpreadsheets = async () => {
    const loaded: SpreadsheetFile[] = [];
    let pageToken = "";
    do {
      const page = parseSpreadsheetPage(await executeProvider("listSpreadsheets", "google.drive.spreadsheets-list", pageToken ? {pageToken} : {}));
      loaded.push(...page.files); pageToken = page.nextPageToken;
    } while (pageToken);
    setSpreadsheets(loaded);
  };
  const loadTabs = async (spreadsheetId: string) => setTabs(parseSheetTabs(await executeProvider("listTabs", "google.sheets.tabs-list", {spreadsheetId})));
  if (!ready) return <p role="status">Waiting for Studio…</p>;
  if (ready.target.kind === "connection") return <SpreadsheetSetupView connection={ready.connection} onConnect={() => void send("oauth.connect", "oauth.connection.manage")} onReconnect={() => void send("oauth.reconnect", "oauth.connection.manage")}/>;
  return <SpreadsheetConfigurationUnit target={ready.target} spreadsheets={spreadsheets} tabs={tabs} busy={busy} onChooseSpreadsheet={() => void loadSpreadsheets()} onLoadTabs={(spreadsheetId) => void loadTabs(spreadsheetId)} onSave={(value) => void send("use.configuration.save", "use.configuration.write", {value})}/>;
}

const style = document.createElement("style");
style.textContent = `:root{font-family:Inter,system-ui;color:#17211b;background:#f5faf6}body{margin:0}.card,.unit{padding:24px}header{display:flex;gap:12px;align-items:center}header img{width:42px;height:42px}h1,h2{font-size:20px;margin:0}p{margin:5px 0;color:#526159}.unit{display:grid;gap:12px}label{display:grid;gap:5px;font-weight:600}input,select,button{font:inherit;padding:9px;border:1px solid #b8c8bc;border-radius:8px}button{background:#188038;color:white;border:0;cursor:pointer}`;
document.head.append(style);
createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
