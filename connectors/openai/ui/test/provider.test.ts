// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";

import { projectOpenAIModelList } from "../src/provider.js";

const now = new Date("2026-09-27T12:00:00Z");

describe("projectOpenAIModelList", () => {
  it("lists newest first and hides non-text families", () => {
    const listing = projectOpenAIModelList({object: "list", data: [
      {id: "gpt-4.1", object: "model", created: 1744000000, owned_by: "system", shutdown_date: null},
      {id: "gpt-6-sol", object: "model", created: 1788000000, owned_by: "system", shutdown_date: null},
      {id: "text-embedding-3-large", object: "model", created: 1705000000, owned_by: "system"},
      {id: "gpt-4o-mini-tts", object: "model", created: 1742000000, owned_by: "system"},
      {id: "whisper-1", object: "model", created: 1677000000, owned_by: "openai-internal"},
      {id: "gpt-transcribe", object: "model", created: 1786000000, owned_by: "system"},
      {id: "gpt-audio-1.5", object: "model", created: 1780000000, owned_by: "system"},
      {id: "gpt-realtime-2.1", object: "model", created: 1787000000, owned_by: "system"},
      {id: "gpt-image-2.5-flare", object: "model", created: 1787500000, owned_by: "system"},
      {id: "dall-e-3", object: "model", created: 1698000000, owned_by: "system"},
      {id: "sora-2", object: "model", created: 1760000000, owned_by: "system"},
      {id: "omni-moderation-latest", object: "model", created: 1731000000, owned_by: "system"},
      {id: "gpt-4o-search-preview", object: "model", created: 1741000000, owned_by: "system"},
      {id: "o3-deep-research", object: "model", created: 1750000000, owned_by: "system"},
      {id: "babbage-002", object: "model", created: 1692000000, owned_by: "system"},
      {id: "davinci-002", object: "model", created: 1692000001, owned_by: "system"},
      {id: "computer-use-preview", object: "model", created: 1741500000, owned_by: "system"},
      {id: "gpt-live-1", object: "model", created: 1787800000, owned_by: "system"},
      {id: "gpt-6-luna", object: "model", created: 1788000100, owned_by: "system"},
      {id: "gpt-6-sol", object: "model", created: 1788000000, owned_by: "system"},
    ]}, now);
    const visible = listing.models.filter((model) => !model.isHiddenByDefault).map((model) => model.id);
    expect(visible).toEqual(["gpt-6-luna", "gpt-6-sol", "gpt-4.1"]);
    expect(listing.models).toHaveLength(19);
    expect(listing.models[0].id).toBe("gpt-6-luna");
  });

  it("filters a fine-tuned model by its base model, not its suffix", () => {
    const listing = projectOpenAIModelList({data: [
      {id: "ft:gpt-4.1-mini-2025-04-14:acme:research-bot:abc123", created: 1770000003},
      {id: "ft:gpt-4.1-mini-2025-04-14:image-lab:audio-tts-notes:def456:ckpt-step-100", created: 1770000002},
      {id: "ft:babbage-002:acme:summaries:ghi789", created: 1770000001},
      {id: "ft:gpt-4o-audio-preview", created: 1770000000},
    ]}, now);
    expect(listing.models.map((model) => [model.id, model.isHiddenByDefault])).toEqual([
      ["ft:gpt-4.1-mini-2025-04-14:acme:research-bot:abc123", false],
      ["ft:gpt-4.1-mini-2025-04-14:image-lab:audio-tts-notes:def456:ckpt-step-100", false],
      ["ft:babbage-002:acme:summaries:ghi789", true],
      ["ft:gpt-4o-audio-preview", true],
    ]);
  });

  it("hides models past their shutdown date and notes announced dates", () => {
    const listing = projectOpenAIModelList({data: [
      {id: "gpt-5.2", created: 1765000000, shutdown_date: "2026-10-23"},
      {id: "gpt-4.5-preview", created: 1740000000, shutdown_date: "2025-07-14"},
      {id: "gpt-5.1", created: 1763000000, shutdown_date: "2026-09-27"},
      {id: "gpt-6-sol", created: 1788000000, shutdown_date: null},
    ]}, now);
    expect(listing.models.map((model) => [model.id, model.isHiddenByDefault, model.detail])).toEqual([
      ["gpt-6-sol", false, undefined],
      ["gpt-5.2", false, "Shuts down on 2026-10-23."],
      ["gpt-5.1", true, "Shut down on 2026-09-27."],
      ["gpt-4.5-preview", true, "Shut down on 2025-07-14."],
    ]);
  });

  it("returns no models for an unexpected shape", () => {
    expect(projectOpenAIModelList({models: []}, now).models).toEqual([]);
  });
});
