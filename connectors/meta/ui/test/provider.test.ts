// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { describe, expect, it } from "vitest";

import { projectMetaModelList } from "../src/provider.js";

describe("projectMetaModelList", () => {
  it("shows Muse Spark models, flags the Contributor tier, and hides other families", () => {
    const listing = projectMetaModelList({object: "list", data: [
      {id: "muse-spark-1.3", object: "model", created: 1790000300, owned_by: "meta"},
      {id: "muse-spark-1.3-contributor", object: "model", created: 1790000300, owned_by: "meta"},
      {id: "muse-image-1.0", object: "model", created: 1790000200, owned_by: "meta"},
      {id: "muse-voice-transcribe-1.0", object: "model", created: 1790000100, owned_by: "meta"},
      {id: "sam-3.1", object: "model", created: 1790000050, owned_by: "meta"},
      {id: "muse-spark-1.2", object: "model", created: 1780000000, owned_by: "meta"},
      {id: "muse-spark-1.3", object: "model", created: 1790000300, owned_by: "meta"},
    ]});
    expect(listing.models.map((model) => [model.id, model.isHiddenByDefault, model.detail])).toEqual([
      ["muse-spark-1.3", false, undefined],
      ["muse-spark-1.3-contributor", false, "Contributor tier: Meta may train on prompts and completions."],
      ["muse-image-1.0", true, undefined],
      ["muse-voice-transcribe-1.0", true, undefined],
      ["sam-3.1", true, undefined],
      ["muse-spark-1.2", false, undefined],
    ]);
  });

  it("returns no models for an unexpected shape", () => {
    expect(projectMetaModelList({models: []}).models).toEqual([]);
  });
});
