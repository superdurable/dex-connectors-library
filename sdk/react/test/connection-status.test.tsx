import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { ConnectionStatus } from "../src/index.js";

describe("ConnectionStatus", () => {
  it("renders a reconnect action without credential material", () => {
    const markup = renderToStaticMarkup(
      <ConnectionStatus provider="Google Sheets" state="expired" detail="Authorization expired" onReconnect={() => undefined} />,
    );
    expect(markup).toContain("Reconnect");
    expect(markup).toContain("data-connection-state=\"expired\"");
    expect(markup).not.toContain("token");
  });

  it("renders connected state without an action", () => {
    const markup = renderToStaticMarkup(<ConnectionStatus provider="OpenAI" state="connected" />);
    expect(markup).toContain("Connected");
    expect(markup).not.toContain("button");
  });
});
