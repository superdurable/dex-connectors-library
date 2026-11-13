// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import { useEffect, useRef, useState, type CSSProperties } from "react";
import { Link, Route, Routes, useLocation, useParams, useSearchParams } from "react-router-dom";

import { GitHubMarkIcon, GitHubStarIcon } from "./GitHubIcons";
import { CategoryIcon, Icon, type IconName } from "./Icons";
import { LanguageIcons, LanguageLogo } from "./LanguageIcons";
import { SiteHeader } from "./SiteHeader";
import { formatStarCount, formatStarredLinkLabel } from "./repositoryStarCount.mjs";
import { useGitHubRepositoryStarCount } from "./useGitHubRepositoryStarCount";
import { useSlidingSegmentIndicator } from "./useSlidingSegmentIndicator";
import { WhatIsDexConnectors } from "./WhatIsDexConnectors";
import {
  allCategories,
  catalogDocumentUrl,
  catalogTotals,
  capabilityMatchesLanguage,
  categoryConnectorCounts,
  categorySearchParameter,
  companyLogoUrl,
  connectorLibraryRepositoryUrl,
  connectorMatchesCategory,
  connectorMatchesQuery,
  connectorMatchesLanguage,
  languageConnectorCounts,
  pythonInstall,
  readCatalogDocument,
  selectedCategoryID,
  type CatalogCapability,
  type CatalogCategory,
  type CatalogConnector,
  type CatalogDocument,
  type PythonInstall,
} from "./catalogModel.mjs";

const logoOrigin = import.meta.env.DEV ? "local" : "published";
const connectorLibraryRepositoryApiUrl = "https://api.github.com/repos/superdurable/dex-connectors-library";
const connectorLibraryStarCountCacheKey = "superdurable-dex-connectors-library-star-count";

type LanguageFilter = "all" | "go" | "python" | "typescript";

const languageFilterLabels: Record<LanguageFilter, string> = {
  all: "All",
  go: "Go",
  python: "Python",
  typescript: "TypeScript",
};

function formatLanguageVersion(language: string, version: string): string {
  return `${language === "typescript" ? "TypeScript" : language === "python" ? "Python" : "Go"} ${version}`;
}

/** Inline style that hands a category's accent hue to its chip, card, or navigation icon. */
function categoryHueStyle(category: CatalogCategory | undefined): CSSProperties | undefined {
  return category ? ({ "--category-hue": category.hue } as CSSProperties) : undefined;
}

/** Returns the directory path that selects categoryID while keeping the other search parameters. */
function categoryLocation(searchParams: URLSearchParams, categoryID: string): string {
  const next = new URLSearchParams(searchParams);
  if (categoryID === allCategories) {
    next.delete(categorySearchParameter);
  } else {
    next.set(categorySearchParameter, categoryID);
  }
  const search = next.toString();
  return search ? `/?${search}` : "/";
}

type LoadState<T> =
  | { status: "loading" }
  | { status: "ready"; value: T }
  | { status: "failed"; message: string };

export function App() {
  const [catalog, setCatalog] = useState<LoadState<CatalogDocument>>({ status: "loading" });
  const location = useLocation();
  const isExplainer = location.pathname === "/what-is-dex-connectors";
  const isDetail = location.pathname.startsWith("/connectors/");
  const siteTabsRef = useRef<HTMLElement>(null);
  const siteTabIndicatorStyle = useSlidingSegmentIndicator(siteTabsRef, isExplainer ? "explainer" : "connectors");

  useEffect(() => {
    const controller = new AbortController();
    const url = catalogDocumentUrl(import.meta.env.DEV, import.meta.env.BASE_URL);
    fetch(url, { signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) {
          throw new Error(`catalog request failed (${response.status})`);
        }
        return response.text();
      })
      .then((text) => setCatalog({ status: "ready", value: readCatalogDocument(text) }))
      .catch((error: unknown) => {
        if (controller.signal.aborted) {
          return;
        }
        setCatalog({ status: "failed", message: errorMessage(error) });
      });
    return () => controller.abort();
  }, []);

  return (
    <>
      <SiteHeader />
      <div className="page">
        <header className={`masthead${isDetail ? " masthead-compact" : ""}`}>
          <div className="masthead-intro">
            <h1 className="directory-title">
              <Link to="/">Dex Connectors</Link>
            </h1>
            <p className="lede">
              Open-source, MIT-licensed connectors that take your process from prototype to production.
              <LedeRepositoryLink />
            </p>
          </div>
          <nav className="site-tabs" aria-label="Connector site" ref={siteTabsRef}>
            <span className="segment-indicator" aria-hidden="true" style={siteTabIndicatorStyle} />
            <Link className={`site-tab${isExplainer ? "" : " active"}`} data-selected={!isExplainer}
              aria-current={isExplainer ? undefined : "page"} to="/">
              Connectors
            </Link>
            <Link className={`site-tab${isExplainer ? " active" : ""}`} data-selected={isExplainer}
              aria-current={isExplainer ? "page" : undefined}
              to="/what-is-dex-connectors">
              What is Dex Connectors
            </Link>
          </nav>
          {catalog.status === "ready" ? <CatalogTotals connectors={catalog.value.connectors} /> : null}
        </header>
        {/* Keyed by path so each page change replays the entrance animation. */}
        <main key={location.pathname} className="route-view">
          <Routes>
            <Route path="/" element={<CatalogPage catalog={catalog} />} />
            <Route path="/connectors/:id" element={<ConnectorPage catalog={catalog} />} />
            <Route path="/what-is-dex-connectors" element={<WhatIsDexConnectors />} />
          </Routes>
        </main>
      </div>
    </>
  );
}

// The pill renders without a count when GitHub is unreachable or rate limited.
function LedeRepositoryLink() {
  const starCount = useGitHubRepositoryStarCount(connectorLibraryRepositoryApiUrl, connectorLibraryStarCountCacheKey);
  return (
    <a
      className="lede-repository-link"
      href={connectorLibraryRepositoryUrl}
      aria-label={formatStarredLinkLabel("GitHub Repo", starCount)}
    >
      <span className="lede-repository-link-name">
        <GitHubMarkIcon size={14} />
        GitHub Repo
      </span>
      {starCount === null ? null : (
        <span className="lede-repository-link-stars">
          <GitHubStarIcon size={12} />
          {formatStarCount(starCount)}
        </span>
      )}
    </a>
  );
}

function CatalogTotals({ connectors }: { connectors: CatalogConnector[] }) {
  const totals = catalogTotals(connectors);
  const items = [
    ["companies", totals.companies],
    ["connectors", totals.connectors],
    ["UI units", totals.uiUnits],
    ["operations", totals.operations],
    ["triggers", totals.triggers],
  ] as const;
  return (
    <dl className="totals">
      {items.map(([label, count]) => (
        <div key={label}>
          <dt>{label}</dt>
          <dd>{count}</dd>
        </div>
      ))}
    </dl>
  );
}

function CatalogPage({ catalog }: { catalog: LoadState<CatalogDocument> }) {
  const [searchParams] = useSearchParams();
  const [query, setQuery] = useState("");
  const [language, setLanguage] = useState<LanguageFilter>("all");
  if (catalog.status === "loading") {
    return <p className="status">Loading catalog…</p>;
  }
  if (catalog.status === "failed") {
    return <p className="status failed">{catalog.message}</p>;
  }
  const { categories, connectors } = catalog.value;
  const categoryID = selectedCategoryID(searchParams.get(categorySearchParameter), categories);
  const selectedCategory = categories.find((category) => category.id === categoryID);
  // Counts follow search and language so every navigation entry predicts its result size.
  const queryMatches = connectors.filter((connector) => connectorMatchesQuery(connector, query));
  const searchAndLanguageMatches = queryMatches.filter((connector) => connectorMatchesLanguage(connector, language));
  const counts = categoryConnectorCounts(categories, searchAndLanguageMatches);
  const matched = searchAndLanguageMatches.filter((connector) => connectorMatchesCategory(connector, categoryID));
  // Language counts follow search and category instead.
  const languageCounts = languageConnectorCounts(
    queryMatches.filter((connector) => connectorMatchesCategory(connector, categoryID)));
  const hasActiveFilters = query.trim() !== "" || language !== "all" || categoryID !== allCategories;
  const clearSearchAndLanguage = () => {
    setQuery("");
    setLanguage("all");
  };

  const hasCategories = categories.length > 0;

  return (
    <div className={`catalog-layout${hasCategories ? "" : " catalog-layout-uncategorized"}`}>
      {hasCategories ? (
        <CategoryNavigation categories={categories} counts={counts} selectedCategoryID={categoryID}
          searchParams={searchParams} />
      ) : null}
      <div className="catalog">
        <div className="catalog-controls">
          <form className="search" role="search" onSubmit={(event) => event.preventDefault()}>
            <label htmlFor="connector-search">Search connectors</label>
            <span className="search-field">
              <Icon name="search" size={18} className="search-icon" />
              <input
                id="connector-search"
                type="search"
                value={query}
                placeholder="Try Gmail, send, or channelPicker"
                onChange={(event) => setQuery(event.target.value)}
              />
            </span>
          </form>
          <LanguageFilterControl language={language} languageCounts={languageCounts} onLanguageChange={setLanguage} />
        </div>
        <div className="results-heading" style={categoryHueStyle(selectedCategory)}>
          <div>
            <h2>
              {selectedCategory ? (
                <span className="results-heading-icon"><CategoryIcon categoryID={selectedCategory.id} size={18} /></span>
              ) : null}
              {selectedCategory ? selectedCategory.name : "All connectors"}
              <span className="results-count">{matched.length}</span>
            </h2>
            {selectedCategory ? <p>{selectedCategory.description}</p> : null}
          </div>
        </div>
        {matched.length === 0 ? (
          <div className="empty-state" role="status">
            <span className="empty-state-icon"><Icon name="inbox" size={28} /></span>
            <p className="empty-state-title">No connector matches these filters</p>
            <p className="empty-state-text">
              {selectedCategory
                ? `Nothing in ${selectedCategory.name} matches the current search and language.`
                : "Nothing matches the current search and language."}
            </p>
            {hasActiveFilters ? (
              <Link className="button-secondary" to="/" onClick={clearSearchAndLanguage}>
                <Icon name="close" size={14} />
                Clear search and filters
              </Link>
            ) : null}
          </div>
        ) : null}
        <ul className="cards">
          {matched.map((connector) => (
            <li key={connector.id}>
              <ConnectorCard connector={connector} language={language} />
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

function CategoryNavigation({ categories, counts, selectedCategoryID: selectedID, searchParams }: {
  categories: CatalogCategory[];
  counts: Map<string, number>;
  selectedCategoryID: string;
  searchParams: URLSearchParams;
}) {
  const selectedLinkRef = useRef<HTMLAnchorElement>(null);
  // On a phone the row scrolls sideways; reveal the selected chip without scrolling the page.
  useEffect(() => {
    const link = selectedLinkRef.current;
    const list = link?.closest("ul");
    if (!link || !list || list.scrollWidth <= list.clientWidth) {
      return;
    }
    const offset = link.getBoundingClientRect().left - list.getBoundingClientRect().left;
    list.scrollLeft += offset - list.clientWidth / 4;
  }, [selectedID]);
  const entries = [
    { id: allCategories, name: "All connectors", hueStyle: undefined as CSSProperties | undefined },
    ...categories.map((category) => ({ id: category.id, name: category.name, hueStyle: categoryHueStyle(category) })),
  ];
  return (
    <nav className="category-nav" aria-label="Categories">
      <p className="category-nav-title">Categories</p>
      <ul>
        {entries.map((entry) => {
          const isSelected = entry.id === selectedID;
          const count = counts.get(entry.id) ?? 0;
          return (
            <li key={entry.id}>
              <Link
                ref={isSelected ? selectedLinkRef : undefined}
                className={`category-link${isSelected ? " selected" : ""}${count === 0 ? " empty" : ""}`}
                to={categoryLocation(searchParams, entry.id)}
                aria-current={isSelected ? "page" : undefined}
                style={entry.hueStyle}
              >
                <span className={`category-link-icon${entry.id === allCategories ? " category-link-icon-all" : ""}`}>
                  {entry.id === allCategories ? <Icon name="grid" size={15} /> : <CategoryIcon categoryID={entry.id} size={15} />}
                </span>
                <span className="category-link-name">{entry.name}</span>
                <span className="category-link-count">{count}</span>
              </Link>
            </li>
          );
        })}
      </ul>
    </nav>
  );
}

function LanguageFilterControl({ language, languageCounts, onLanguageChange }: {
  language: LanguageFilter;
  languageCounts: Map<LanguageFilter, number>;
  onLanguageChange: (language: LanguageFilter) => void;
}) {
  const fieldsetRef = useRef<HTMLFieldSetElement>(null);
  const indicatorStyle = useSlidingSegmentIndicator(fieldsetRef, language);
  return (
    <fieldset className="language-filter" ref={fieldsetRef}>
      <legend className="visually-hidden">Language</legend>
      <span className="segment-indicator" aria-hidden="true" style={indicatorStyle} />
      {(["all", "go", "python", "typescript"] as const).map((option) => (
        <label key={option} className={language === option ? "selected" : ""} data-selected={language === option}>
          <input type="radio" name="language" value={option} checked={language === option}
            onChange={() => onLanguageChange(option)} />
          {option === "all" ? null : (
            <span className={`language-filter-logo language-icon-${option}`}><LanguageLogo language={option} /></span>
          )}
          {languageFilterLabels[option]}
          <span className="language-filter-count">{languageCounts.get(option)}</span>
        </label>
      ))}
    </fieldset>
  );
}

function ConnectorCard({ connector, language }: { connector: CatalogConnector; language: LanguageFilter }) {
  const primaryCategory = connector.categories[0];
  return (
    <Link className="card" to={`/connectors/${connector.id}`} style={categoryHueStyle(primaryCategory)}>
      <span className="card-meta">
        <span className="card-logo">
          <img src={companyLogoUrl(connector.companyDirectory, logoOrigin)} alt="" width={40} height={40} />
        </span>
        <LanguageIcons languages={connectorLanguageNames(connector.languages)} versions={connector.languages} />
      </span>
      <span className="card-heading">
        <span className="card-title">{connector.name}</span>
        <span className="card-company">{connector.company}</span>
      </span>
      <span className="description">{connector.description}</span>
      <span className="card-footer">
        {primaryCategory ? <CategoryLabel category={primaryCategory} /> : null}
        <CapabilityChips connector={connector} language={language} />
      </span>
    </Link>
  );
}

/** A non-interactive category label; cards are links, so a nested link would be invalid. */
function CategoryLabel({ category }: { category: CatalogCategory }) {
  return (
    <span className="category-chip" style={categoryHueStyle(category)}>
      <CategoryIcon categoryID={category.id} size={13} />
      {category.name}
    </span>
  );
}

function CapabilityChips({ connector, language }: {
  connector: CatalogConnector;
  language: LanguageFilter;
}) {
  const chips = [
    ...connector.uiUnits.map((capability) => ({ ...capability, key: `ui-unit-${capability.name}` })),
    ...connector.triggers.map((capability) => ({ ...capability, key: `trigger-${capability.name}` })),
    ...connector.operations.map((capability) => ({ ...capability, key: `operation-${capability.name}` })),
  ].filter((capability) => capabilityMatchesLanguage(capability, language));
  if (chips.length === 0) {
    return null;
  }
  const visibleChips = chips.slice(0, 2);
  const hiddenChipCount = chips.length - visibleChips.length;
  return (
    <span className="chips">
      {visibleChips.map((capability) => (
        <span key={capability.key} className={`kind kind-${capability.kind}`}>
          {capability.name}
        </span>
      ))}
      {hiddenChipCount > 0 ? (
        <span className="kind kind-more" aria-label={`${hiddenChipCount} more capabilities`}>
          +{hiddenChipCount}
        </span>
      ) : null}
    </span>
  );
}

function ConnectorPage({ catalog }: { catalog: LoadState<CatalogDocument> }) {
  const { id } = useParams();
  const connector =
    catalog.status === "ready" ? catalog.value.connectors.find((item) => item.id === id) : undefined;

  if (catalog.status === "loading") {
    return <p className="status">Loading catalog…</p>;
  }
  if (catalog.status === "failed") {
    return <p className="status failed">{catalog.message}</p>;
  }
  if (!connector) {
    return (
      <p className="status failed">
        This catalog does not include that connector. <Link to="/">Back to the directory</Link>
      </p>
    );
  }

  const goInstallCommand =
    `go get github.com/superdurable/dex-connectors-library/${connector.directory}/go@${connector.languages.go}`;
  return (
    <article className="detail" style={categoryHueStyle(connector.categories[0])}>
      <p className="back">
        <Link to="/">
          <Icon name="arrowLeft" size={16} />
          All connectors
        </Link>
      </p>
      <header className="detail-heading">
        <span className="detail-logo">
          <img src={companyLogoUrl(connector.companyDirectory, logoOrigin)} alt="" width={64} height={64} />
        </span>
        <div>
          <p className="company-name">{connector.company}</p>
          <h2>{connector.name}</h2>
          {connector.categories.length > 0 ? (
            <p className="detail-categories">
              {connector.categories.map((category, index) => (
                <Link key={category.id} className="category-chip category-chip-link" style={categoryHueStyle(category)}
                  to={`/?${categorySearchParameter}=${category.id}`}
                  title={index === 0 ? "Primary category" : "Secondary category"}>
                  <CategoryIcon categoryID={category.id} size={13} />
                  {category.name}
                </Link>
              ))}
            </p>
          ) : null}
        </div>
      </header>
      <p className="description detail-description">{connector.description}</p>
      <aside className="detail-aside" aria-label="Connector facts">
        <dl className="detail-facts">
          <div>
            <dt>Releases</dt>
            {Object.entries(connector.languages).map(([language, version]) => (
              <dd key={language}>
                <LanguageIcons languages={[language as "go" | "python" | "typescript"]} versions={connector.languages} />
                {formatLanguageVersion(language, version)}
              </dd>
            ))}
          </div>
          <div>
            <dt>Directory</dt>
            <dd><code>{connector.directory}</code></dd>
          </div>
        </dl>
        <a className="repository-source-link" href={`${connectorLibraryRepositoryUrl}/tree/main/${connector.directory}`}>
          <GitHubMarkIcon />
          View source
        </a>
      </aside>
      <div className="detail-content">
        <section className="install">
          <h3>
            <span className="section-icon"><Icon name="terminal" size={16} /></span>
            Install
          </h3>
          <InstallCommand command={goInstallCommand} label="Go install command" />
          <PythonInstallCommand install={pythonInstall(connector)} />
        </section>
        <CapabilityList title="UI units" icon="layout" capabilities={connector.uiUnits} versions={connector.languages} showLanguages={false} />
        <CapabilityList title="Triggers" icon="bolt" capabilities={connector.triggers} versions={connector.languages} />
        <CapabilityList title="Operations" icon="code" capabilities={connector.operations} versions={connector.languages} />
      </div>
    </article>
  );
}

type CopyState = "idle" | "copied" | "failed";

/** Shows one install command with a copy button; `label` names the command for assistive technology. */
function InstallCommand({ command, label }: { command: string; label: string }) {
  const [copyState, setCopyState] = useState<CopyState>("idle");
  const commandRef = useRef<HTMLElement>(null);
  useEffect(() => {
    if (copyState === "idle") {
      return;
    }
    const timer = window.setTimeout(() => setCopyState("idle"), copyState === "failed" ? 4000 : 1600);
    return () => window.clearTimeout(timer);
  }, [copyState]);
  // Without clipboard access (insecure origin, denied permission), select the text for a manual copy.
  const reportCopyFailure = () => {
    if (commandRef.current) {
      window.getSelection()?.selectAllChildren(commandRef.current);
    }
    setCopyState("failed");
  };
  const copy = () => {
    const clipboard = typeof navigator === "undefined" ? undefined : navigator.clipboard;
    if (!clipboard || typeof clipboard.writeText !== "function") {
      reportCopyFailure();
      return;
    }
    try {
      clipboard.writeText(command).then(() => setCopyState("copied"), reportCopyFailure);
    } catch {
      reportCopyFailure();
    }
  };
  const announcement = copyState === "copied" ? `Copied the ${label}`
    : copyState === "failed" ? `Copy failed. The ${label} is selected; copy it with your keyboard.` : "";
  return (
    <div className="install-command">
      <code ref={commandRef}>{command}</code>
      <button type="button" className={`copy-button${copyState === "failed" ? " copy-button-failed" : ""}`}
        onClick={copy} aria-label={`Copy ${label}`}>
        <Icon name={copyState === "copied" ? "check" : "copy"} size={15} />
        <span aria-hidden="true">
          {copyState === "copied" ? "Copied" : copyState === "failed" ? "Copy manually" : "Copy"}
        </span>
      </button>
      <span className="visually-hidden" role="status">{announcement}</span>
    </div>
  );
}

function PythonInstallCommand({ install }: { install: PythonInstall | null }) {
  if (!install) {
    return null;
  }
  return (
    <>
      <InstallCommand command={install.command} label="pip install command" />
      {install.isPyPIPublished ? null : (
        <p className="install-note">
          The PyPI upload is pending. Install from the release tag, or download the wheel from
          the <a href={install.releaseUrl}>GitHub release</a>.
        </p>
      )}
    </>
  );
}

const capabilityKindIcons: Record<string, IconName> = {
  "ui-unit": "layout",
  trigger: "bolt",
  query: "eye",
  mutation: "pencil",
};

function CapabilityList({ title, icon, capabilities, versions, showLanguages = true }: {
  title: string;
  icon: IconName;
  capabilities: CatalogCapability[];
  versions: CatalogConnector["languages"];
  showLanguages?: boolean;
}) {
  if (capabilities.length === 0) {
    return null;
  }
  return (
    <section className="capabilities">
      <h3>
        <span className="section-icon"><Icon name={icon} size={16} /></span>
        {title}
        <span className="results-count">{capabilities.length}</span>
      </h3>
      <ul className="operations">
        {capabilities.map((capability) => (
          <li key={`${capability.kind}-${capability.name}`}>
            <span className={`kind kind-${capability.kind}`}>
              <Icon name={capabilityKindIcons[capability.kind] ?? "code"} size={13} />
              {capabilityKindLabel(capability.kind)}
            </span>
            <span>
              <strong>{capability.name}</strong>
              <span className="description">{capability.description}</span>
            </span>
            {showLanguages ? <LanguageIcons languages={capability.languages} versions={versions} /> : null}
          </li>
        ))}
      </ul>
    </section>
  );
}

function connectorLanguageNames(versions: CatalogConnector["languages"]): Array<"go" | "python" | "typescript"> {
  return (["go", "python", "typescript"] as const).filter((language) => Boolean(versions[language]));
}

function capabilityKindLabel(kind: string): string {
  return kind === "ui-unit" ? "UI unit" : kind;
}

function errorMessage(error: unknown): string {
  if (error instanceof Error && error.message) {
    return error.message;
  }
  return "The request failed.";
}
