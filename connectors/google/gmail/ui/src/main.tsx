import { StrictMode, useEffect, useMemo, useState } from "react";
import { createRoot } from "react-dom/client";
import {
  connectorStudioHostAPIVersion,
  isConnectorStudioMessage,
  type ConnectorStudioCommand,
  type ConnectorStudioHostReady,
} from "@superdurable/dex-connectors-react";
import { GmailSetupView } from "./setup.js";

const connectorId = "gmail";

function ConnectorApp() {
  const [ready, setReady] = useState<ConnectorStudioHostReady>();
  const [busy, setBusy] = useState(false);
  const pending = useMemo(() => new Set<string>(), []);
  useEffect(() => {
    const receive = (event: MessageEvent<unknown>) => {
      if (event.source !== window.parent || !isConnectorStudioMessage(event.data) || event.data.connectorId !== connectorId) return;
      if (event.data.type === "connector.host.ready") { setReady(event.data); return; }
      if (event.data.type === "connector.command.result" && ready && event.data.sessionNonce === ready.sessionNonce) {
        pending.delete(event.data.requestId); setBusy(pending.size > 0);
      }
    };
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, [pending, ready]);
  const send = (command: ConnectorStudioCommand["command"]) => {
    if (!ready || !ready.capabilities.includes("oauth.connection.manage")) return;
    const requestId = crypto.randomUUID(); pending.add(requestId); setBusy(true);
    const message: ConnectorStudioCommand = { type: "connector.command", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: ready.sessionNonce, connectorId, requestId, command };
    window.parent.postMessage(message, "*");
  };
  if (!ready) return <p role="status">Waiting for Studio…</p>;
  return <GmailSetupView connection={ready.connection} busy={busy} onCommand={send}/>;
}

const style = document.createElement("style");
style.textContent = `:root{font-family:Inter,system-ui;color:#281b1b;background:#fff8f7}body{margin:0}.card{padding:24px}header{display:flex;gap:12px;align-items:center}header img{width:42px;height:42px}h1{font-size:20px;margin:0}p{margin:5px 0;color:#675252}.controls{display:grid;gap:12px;margin-top:18px}.note{font-size:13px}button{font:inherit;padding:9px;border-radius:8px;cursor:pointer}.secondary{background:white;color:#8a2525;border:1px solid #d8b8b8}`;
document.head.append(style);
createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
