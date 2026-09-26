import { connectorStudioHostAPIVersion, type ConnectorStudioFrameResize, type ConnectorStudioHostReady } from "./host-api.js";

export function observeConnectorStudioFrameAutoHeight(ready: ConnectorStudioHostReady): () => void {
  const content = document.getElementById("root") ?? document.body;
  let animationFrame: number | undefined;
  let lastHeight = 0;
  const publishHeight = () => {
    animationFrame = undefined;
    const measuredHeight = Math.ceil(content.getBoundingClientRect().height) + 8;
    const height = Math.min(4096, Math.max(80, measuredHeight));
    if (height === lastHeight) return;
    lastHeight = height;
    window.parent.postMessage({
      type: "connector.frame.resize",
      protocolVersion: connectorStudioHostAPIVersion,
      sessionNonce: ready.sessionNonce,
      connectorId: ready.connectorId,
      height,
    } satisfies ConnectorStudioFrameResize, "*");
  };
  const scheduleHeight = () => {
    if (animationFrame !== undefined) cancelAnimationFrame(animationFrame);
    animationFrame = requestAnimationFrame(publishHeight);
  };
  const observer = typeof ResizeObserver === "undefined" ? undefined : new ResizeObserver(scheduleHeight);
  observer?.observe(content);
  scheduleHeight();
  return () => {
    observer?.disconnect();
    if (animationFrame !== undefined) cancelAnimationFrame(animationFrame);
  };
}
