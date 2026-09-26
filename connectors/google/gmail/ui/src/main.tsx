import { StrictMode, useEffect, useState } from "react";
import { createRoot } from "react-dom/client";
import { connectorStudioHostAPIVersion, isConnectorStudioMessage, type ConnectorStudioCommand, type ConnectorStudioHostReady } from "@superdurable/dex-connectors-react";
import { GmailSetupView } from "./setup.js";
import { GmailConfigurationUnit } from "./units.js";

const connectorId = "gmail";

function ConnectorApp() {
  const [ready, setReady] = useState<ConnectorStudioHostReady>();
  useEffect(() => {
    const receive = (event: MessageEvent<unknown>) => {
      if (event.source === window.parent && isConnectorStudioMessage(event.data) && event.data.type === "connector.host.ready" && event.data.connectorId === connectorId) setReady(event.data);
    };
    window.addEventListener("message", receive);
    return () => window.removeEventListener("message", receive);
  }, []);
  const send = (command: ConnectorStudioCommand["command"], input?: Record<string, unknown>) => {
    if (!ready || !ready.capabilities.includes(command.startsWith("oauth.") ? "oauth.connection.manage" : "use.configuration.write")) return;
    window.parent.postMessage({type: "connector.command", protocolVersion: connectorStudioHostAPIVersion, sessionNonce: ready.sessionNonce, connectorId, requestId: crypto.randomUUID(), command, input} satisfies ConnectorStudioCommand, "*");
  };
  if (!ready) return <p role="status">Waiting for Studio…</p>;
  if (ready.target.kind === "connection") return <GmailSetupView connection={ready.connection} onConnect={() => send("oauth.connect")} onReconnect={() => send("oauth.reconnect")}/>;
  return <GmailConfigurationUnit target={ready.target} onSave={(value) => send("use.configuration.save", {value})}/>;
}

const style = document.createElement("style");
style.textContent = `:root{font-family:Inter,system-ui;color:#281b1b;background:#fff8f7}body{margin:0}.card,.unit{padding:24px}header{display:flex;gap:12px;align-items:center}header img{width:42px;height:42px}h1,h2{font-size:20px;margin:0}p{margin:5px 0;color:#675252}.unit{display:grid;gap:12px}.unit label{display:grid;gap:5px;font-weight:600}.unit input{font:inherit;padding:9px;border:1px solid #d8b8b8;border-radius:8px}button{font:inherit;padding:9px;border-radius:8px;cursor:pointer}`;
document.head.append(style);
createRoot(document.getElementById("root")!).render(<StrictMode><ConnectorApp/></StrictMode>);
