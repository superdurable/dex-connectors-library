import { mkdirSync, readFileSync, writeFileSync } from "node:fs";
import path from "node:path";
import { parseArgs } from "node:util";
import { pathToFileURL } from "node:url";

const handlePattern = /^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$/;
const maximumAvatarBytes = 512 * 1024;
const themes = {
  light: {
    background: "#f8fbf5",
    surface: "#ffffff",
    edge: "#dce8d8",
    ink: "#183c25",
    muted: "#607263",
    accent: "#2e6d3a",
    ring: "#c9dbc5",
    placeholder: "#e9f2e5",
  },
  dark: {
    background: "#101a12",
    surface: "#142519",
    edge: "#2d4932",
    ink: "#edf7eb",
    muted: "#a7bca8",
    accent: "#72bd7b",
    ring: "#416348",
    placeholder: "#263d2b",
  },
};

export class GitHubAPI {
  constructor({ repository, ref, token = "", fetchImpl = fetch }) {
    if (!/^[A-Za-z0-9_.-]+\/[A-Za-z0-9_.-]+$/.test(repository)) {
      throw new Error("invalid GitHub repository");
    }
    if (!ref || /[\s?&#]/.test(ref)) {
      throw new Error("invalid Git ref");
    }
    this.repository = repository;
    this.ref = ref;
    this.token = token;
    this.fetchImpl = fetchImpl;
  }

  async listCommits(page, perPage = 100) {
    const url = new URL(`https://api.github.com/repos/${this.repository}/commits`);
    url.searchParams.set("sha", this.ref);
    url.searchParams.set("per_page", String(perPage));
    url.searchParams.set("page", String(page));
    const response = await request(this.fetchImpl, url, {
      headers: githubHeaders(this.token),
    });
    const data = await response.json();
    if (!Array.isArray(data)) {
      throw new Error("GitHub commits response is not an array");
    }
    return data;
  }
}

export async function collectContributors(api) {
  const contributors = new Map();
  for (let page = 1; ; page += 1) {
    const commits = await api.listCommits(page, 100);
    for (const commit of commits) {
      const author = commit?.author;
      const login = author?.login;
      if (
        author?.type !== "User" ||
        typeof login !== "string" ||
        !handlePattern.test(login) ||
        login.toLowerCase().endsWith("[bot]")
      ) {
        continue;
      }
      const key = login.toLowerCase();
      const current = contributors.get(key) ?? {
        login,
        commits: 0,
        avatarUrl: author.avatar_url ?? "",
        profileUrl: `https://github.com/${login}`,
      };
      current.commits += 1;
      if (!current.avatarUrl && author.avatar_url) {
        current.avatarUrl = author.avatar_url;
      }
      contributors.set(key, current);
    }
    if (commits.length < 100) {
      break;
    }
  }
  return [...contributors.values()].sort(
    (left, right) => right.commits - left.commits ||
      left.login.localeCompare(right.login, "en", { sensitivity: "base" }),
  );
}

export async function addAvatars(contributors, previous = [], loadAvatar = downloadAvatar) {
  const cache = new Map();
  for (const contributor of previous) {
    if (typeof contributor?.login !== "string") {
      continue;
    }
    try {
      cache.set(contributor.login.toLowerCase(), validateAvatarDataURL(contributor.avatarDataUrl));
    } catch {
      // Ignore invalid generated data instead of carrying it into a new asset.
    }
  }

  return mapConcurrent(contributors, 6, async (contributor) => {
    let avatarDataUrl = "";
    try {
      avatarDataUrl = await loadAvatar(contributor.avatarUrl);
    } catch (error) {
      avatarDataUrl = cache.get(contributor.login.toLowerCase()) ?? "";
      console.warn(`Avatar fallback for @${contributor.login}: ${error.name ?? "Error"}`);
    }
    return { ...contributor, avatarDataUrl };
  });
}

export function renderContributors(themeName, contributors) {
  const theme = themes[themeName];
  if (!theme) {
    throw new Error(`unknown theme: ${themeName}`);
  }
  const columns = 10;
  const rows = Math.max(1, Math.ceil(contributors.length / columns));
  const rowHeight = 84;
  const headerHeight = 170;
  const footerHeight = 52;
  const height = headerHeight + rows * rowHeight + footerHeight;
  const people = [];
  for (let row = 0; row < rows; row += 1) {
    const group = contributors.slice(row * columns, (row + 1) * columns);
    const spacing = 90;
    const rowWidth = Math.max(0, (group.length - 1) * spacing);
    const startX = 500 - rowWidth / 2;
    for (const [column, contributor] of group.entries()) {
      const x = startX + column * spacing;
      const y = headerHeight + row * rowHeight + 30;
      const clipID = `avatar-${row}-${column}`;
      const commitLabel = `${contributor.commits} ${contributor.commits === 1 ? "commit" : "commits"} on main`;
      const image = contributor.avatarDataUrl
        ? `<image x="${x - 29}" y="${y - 29}" width="58" height="58" clip-path="url(#${clipID})" preserveAspectRatio="xMidYMid slice" href="${escapeXML(contributor.avatarDataUrl)}"/>`
        : `<circle cx="${x}" cy="${y}" r="29" fill="${theme.placeholder}"/>${text(x, y + 9, contributor.login[0].toUpperCase(), 25, theme.ink, 700, "middle")}`;
      people.push(`<a href="${escapeXML(contributor.profileUrl)}" target="_blank"><g><title>@${escapeXML(contributor.login)} — ${escapeXML(commitLabel)}</title><defs><clipPath id="${clipID}"><circle cx="${x}" cy="${y}" r="29"/></clipPath></defs>${image}<circle cx="${x}" cy="${y}" r="30.5" fill="none" stroke="${theme.ring}" stroke-width="1.5"/></g></a>`);
    }
  }

  const contributorLabel = `${contributors.length} ${contributors.length === 1 ? "CONTRIBUTOR" : "CONTRIBUTORS"}`;
  return `<?xml version="1.0" encoding="UTF-8"?>
<svg xmlns="http://www.w3.org/2000/svg" width="1000" height="${height}" viewBox="0 0 1000 ${height}" role="img" aria-labelledby="title description">
  <title id="title">Dex Connectors — ${contributors.length} contributors</title>
  <desc id="description">GitHub users with attributed commits on the main branch.</desc>
  <rect width="1000" height="${height}" rx="24" fill="${theme.background}"/>
  <rect x="1" y="1" width="998" height="${height - 2}" rx="23" fill="none" stroke="${theme.edge}" stroke-width="2"/>
  <rect x="0" y="0" width="10" height="${height}" rx="5" fill="${theme.accent}"/>
  ${text(48, 48, "DEX CONNECTORS / COMMUNITY", 12, theme.accent, 700, "start", 1.5)}
  ${text(48, 94, "Contributors", 36, theme.ink, 700)}
  ${text(48, 128, "GitHub users with attributed commits on main", 17, theme.muted, 400)}
  <rect x="760" y="58" width="192" height="46" rx="23" fill="${theme.surface}" stroke="${theme.edge}"/>
  ${text(856, 87, contributorLabel, 13, theme.accent, 700, "middle", 0.7)}
  ${people.join("\n  ")}
  <path d="M40 ${height - 42} H960" stroke="${theme.edge}"/>
  ${text(48, height - 18, "Counts follow the repository's main branch", 12, theme.muted, 400)}
  ${text(952, height - 18, "DEX CONNECTORS", 11, theme.accent, 700, "end", 0.8)}
</svg>`;
}

export function validateAvatarDataURL(value) {
  if (typeof value !== "string" || !value.startsWith("data:image/")) {
    throw new Error("invalid cached avatar data URL");
  }
  const match = value.match(/^data:(image\/(?:png|jpeg|gif|webp));base64,([A-Za-z0-9+/]+={0,2})$/);
  if (!match) {
    throw new Error("unsupported cached avatar data URL");
  }
  const data = Buffer.from(match[2], "base64");
  if (data.length > maximumAvatarBytes || detectRasterMime(data) !== match[1]) {
    throw new Error("invalid cached avatar bytes");
  }
  return value;
}

export function rasterDataURL(data) {
  if (!Buffer.isBuffer(data) || data.length === 0 || data.length > maximumAvatarBytes) {
    throw new Error("avatar exceeds the allowed size or is empty");
  }
  const mime = detectRasterMime(data);
  if (!mime) {
    throw new Error("avatar is not a supported raster image");
  }
  return `data:${mime};base64,${data.toString("base64")}`;
}

async function downloadAvatar(value) {
  const url = new URL(value);
  if (url.protocol !== "https:" || url.hostname !== "avatars.githubusercontent.com") {
    throw new Error("unexpected avatar host");
  }
  url.searchParams.set("s", "128");
  const response = await request(fetch, url);
  const declaredLength = Number(response.headers.get("content-length") ?? 0);
  if (declaredLength > maximumAvatarBytes) {
    throw new Error("avatar exceeds the allowed size");
  }
  const data = Buffer.from(await response.arrayBuffer());
  return rasterDataURL(data);
}

async function request(fetchImpl, url, options = {}) {
  let lastError;
  for (let attempt = 0; attempt < 3; attempt += 1) {
    try {
      const response = await fetchImpl(url, {
        ...options,
        signal: AbortSignal.timeout(25_000),
      });
      if (response.ok) {
        return response;
      }
      if (response.status < 500 || attempt === 2) {
        throw new Error(`request failed with status ${response.status}`);
      }
      lastError = new Error(`request failed with status ${response.status}`);
    } catch (error) {
      lastError = error;
      if (attempt === 2) {
        throw error;
      }
    }
    await new Promise((resolve) => setTimeout(resolve, (attempt + 1) * 250));
  }
  throw lastError;
}

function githubHeaders(token) {
  return {
    Accept: "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28",
    "User-Agent": "dex-connectors-contributors",
    ...(token ? { Authorization: `Bearer ${token}` } : {}),
  };
}

function detectRasterMime(data) {
  if (data.subarray(0, 8).equals(Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]))) {
    return "image/png";
  }
  if (data.length >= 3 && data[0] === 0xff && data[1] === 0xd8 && data[2] === 0xff) {
    return "image/jpeg";
  }
  const header = data.subarray(0, 6).toString("ascii");
  if (header === "GIF87a" || header === "GIF89a") {
    return "image/gif";
  }
  if (data.length >= 12 && data.subarray(0, 4).toString("ascii") === "RIFF" && data.subarray(8, 12).toString("ascii") === "WEBP") {
    return "image/webp";
  }
  return "";
}

function readPrevious(output) {
  try {
    const data = JSON.parse(readFileSync(path.join(output, "contributors.json"), "utf8"));
    return Array.isArray(data.contributors) ? data.contributors : [];
  } catch (error) {
    if (error.code === "ENOENT") {
      return [];
    }
    throw error;
  }
}

async function mapConcurrent(items, concurrency, mapper) {
  const result = new Array(items.length);
  let next = 0;
  async function worker() {
    while (next < items.length) {
      const index = next;
      next += 1;
      result[index] = await mapper(items[index], index);
    }
  }
  await Promise.all(Array.from({ length: Math.min(concurrency, items.length) }, worker));
  return result;
}

function text(x, y, value, size, color, weight = 400, anchor = "start", spacing = undefined) {
  const letterSpacing = spacing === undefined ? "" : ` letter-spacing="${spacing}"`;
  return `<text x="${x}" y="${y}" font-family="Arial,Helvetica,sans-serif" font-size="${size}" font-weight="${weight}" fill="${color}" text-anchor="${anchor}"${letterSpacing}>${escapeXML(value)}</text>`;
}

function escapeXML(value) {
  return String(value)
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&apos;");
}

async function main() {
  const { values } = parseArgs({
    options: {
      output: { type: "string" },
      repository: { type: "string", default: "superdurable/dex-connectors-library" },
      ref: { type: "string", default: "main" },
    },
  });
  if (!values.output) {
    throw new Error("usage: contributors.mjs --output PATH [--repository OWNER/REPO] [--ref REF]");
  }
  const output = path.resolve(values.output);
  const api = new GitHubAPI({
    repository: values.repository,
    ref: values.ref,
    token: process.env.GH_TOKEN ?? "",
  });
  const contributors = await collectContributors(api);
  if (contributors.length === 0) {
    throw new Error("no linked human contributors found on the requested ref");
  }
  const withAvatars = await addAvatars(contributors, readPrevious(output));
  mkdirSync(output, { recursive: true });
  writeFileSync(path.join(output, "contributors-light.svg"), `${renderContributors("light", withAvatars)}\n`);
  writeFileSync(path.join(output, "contributors-dark.svg"), `${renderContributors("dark", withAvatars)}\n`);
  writeFileSync(path.join(output, "contributors.json"), `${JSON.stringify({
    repository: values.repository,
    ref: values.ref,
    contributors: withAvatars,
  }, null, 2)}\n`);
  console.log(`${withAvatars.length} contributors on ${values.ref}`);
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  await main();
}
