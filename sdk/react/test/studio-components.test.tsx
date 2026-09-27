// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { renderToStaticMarkup } from "react-dom/server";
import { describe, expect, it } from "vitest";

import { StudioField } from "../src/index.js";

describe("StudioField", () => {
  it("describes the control with its hint instead of naming it with the hint", () => {
    const markup = renderToStaticMarkup(<StudioField hint="Use an ID when the item is not listed." label="Item ID">
      <input aria-describedby="item-error" type="text"/>
    </StudioField>);
    const label = markup.match(/<label>.*<\/label>/)?.[0] ?? "";
    const hintId = markup.match(/<small id="([^"]+)">Use an ID when the item is not listed\.<\/small>/)?.[1];
    expect(label).toContain("Item ID");
    expect(label).not.toContain("not listed");
    expect(hintId).toBeTruthy();
    expect(label).toContain(`aria-describedby="item-error ${hintId}"`);
  });

  it("renders no description without a hint", () => {
    const markup = renderToStaticMarkup(<StudioField label="Tab"><select/></StudioField>);
    expect(markup).toBe('<div class="studio-field"><label><span>Tab</span><select></select></label></div>');
  });
});
