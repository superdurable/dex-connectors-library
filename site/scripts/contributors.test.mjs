import assert from "node:assert/strict";
import test from "node:test";

import { Resvg } from "@resvg/resvg-js";

import {
  GitHubAPI,
  addAvatars,
  collectContributors,
  rasterDataURL,
  renderContributors,
  validateAvatarDataURL,
} from "./contributors.mjs";

const png = Buffer.from("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+a7mgAAAAASUVORK5CYII=", "base64");

function commit(login, { type = "User", avatarUrl = `https://avatars.githubusercontent.com/u/${login?.length ?? 0}` } = {}) {
  return {
    author: login === null ? null : {
      login,
      type,
      avatar_url: avatarUrl,
    },
  };
}

test("collects every page, groups GitHub identities, excludes bots, and sorts deterministically", async () => {
  const firstPage = Array.from({ length: 100 }, () => commit("Alice"));
  firstPage[1] = commit("alice");
  firstPage[2] = commit("dependabot[bot]", { type: "Bot" });
  firstPage[3] = commit(null);
  const api = {
    calls: [],
    async listCommits(page, perPage) {
      this.calls.push([page, perPage]);
      return page === 1 ? firstPage : [commit("Bob"), commit("Bob"), commit("Carol")];
    },
  };
  const result = await collectContributors(api);
  assert.deepEqual(api.calls, [[1, 100], [2, 100]]);
  assert.deepEqual(result.map(({ login, commits }) => ({ login, commits })), [
    { login: "Alice", commits: 98 },
    { login: "Bob", commits: 2 },
    { login: "Carol", commits: 1 },
  ]);
});

test("requests commits from the configured main ref", async () => {
  let requested;
  const api = new GitHubAPI({
    repository: "superdurable/dex-connectors-library",
    ref: "main",
    fetchImpl: async (url) => {
      requested = new URL(url);
      return new Response("[]", { status: 200, headers: { "content-type": "application/json" } });
    },
  });
  await api.listCommits(3, 100);
  assert.equal(requested.pathname, "/repos/superdurable/dex-connectors-library/commits");
  assert.equal(requested.searchParams.get("sha"), "main");
  assert.equal(requested.searchParams.get("page"), "3");
});

test("avatar refresh failures reuse only a valid raster cache", async () => {
  const cached = rasterDataURL(png);
  const contributors = [{
    login: "Alice", commits: 2,
    avatarUrl: "https://avatars.githubusercontent.com/u/1",
    profileUrl: "https://github.com/Alice",
  }];
  const result = await addAvatars(
    contributors,
    [{ login: "alice", avatarDataUrl: cached }],
    async () => { throw new Error("offline"); },
  );
  assert.equal(result[0].avatarDataUrl, cached);
  const withoutUnsafeCache = await addAvatars(
    contributors,
    [{ login: "Alice", avatarDataUrl: "data:image/svg+xml;base64,PHN2Zy8+" }],
    async () => { throw new Error("offline"); },
  );
  assert.equal(withoutUnsafeCache[0].avatarDataUrl, "");
});

test("accepts supported raster bytes and rejects SVG or malformed cached data", () => {
  const encoded = rasterDataURL(png);
  assert.equal(validateAvatarDataURL(encoded), encoded);
  assert.throws(() => rasterDataURL(Buffer.from("<svg onload='bad()'/>", "utf8")), /supported raster/);
  const oversized = Buffer.concat([png, Buffer.alloc(512 * 1024)]);
  assert.throws(() => rasterDataURL(oversized), /allowed size/);
  assert.throws(() => validateAvatarDataURL("data:image/png;base64,PHN2Zy8+"), /invalid cached avatar bytes/);
});

test("renders safe responsive light and dark SVG cards for growing rosters", () => {
  const contributors = Array.from({ length: 21 }, (_, index) => ({
    login: index === 0 ? "script-alert" : `person-${index}`,
    commits: 30 - index,
    avatarDataUrl: rasterDataURL(png),
    profileUrl: `https://github.com/person-${index}`,
  }));
  for (const theme of ["light", "dark"]) {
    const svg = renderContributors(theme, contributors);
    assert.match(svg, /21 CONTRIBUTORS/);
    assert.match(svg, /height="474"/);
    assert.doesNotMatch(svg, /<script/i);
    const rendered = new Resvg(svg).render().asPng();
    assert.equal(rendered.subarray(0, 8).toString("hex"), "89504e470d0a1a0a");
  }
});
