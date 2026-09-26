import { useEffect, useState } from "react";
import { Link, Route, Routes, useParams } from "react-router-dom";

import { DexMark } from "./DexMark";
import {
  catalogDocumentUrl,
  catalogTotals,
  companyLogoUrl,
  connectorMatchesQuery,
  groupCatalog,
  readCatalog,
  type CatalogCapability,
  type CatalogConnector,
} from "./catalogModel.mjs";

const logoOrigin = import.meta.env.DEV ? "local" : "published";

type LoadState<T> =
  | { status: "loading" }
  | { status: "ready"; value: T }
  | { status: "failed"; message: string };

export function App() {
  const [catalog, setCatalog] = useState<LoadState<CatalogConnector[]>>({ status: "loading" });

  useEffect(() => {
    const controller = new AbortController();
    const url = catalogDocumentUrl(
      import.meta.env.DEV,
      import.meta.env.BASE_URL,
      import.meta.env.VITE_CATALOG_VERSION,
    );
    fetch(url, { cache: "no-store", signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) {
          throw new Error(`catalog request failed (${response.status})`);
        }
        return response.text();
      })
      .then((text) => setCatalog({ status: "ready", value: readCatalog(text) }))
      .catch((error: unknown) => {
        if (controller.signal.aborted) {
          return;
        }
        setCatalog({ status: "failed", message: errorMessage(error) });
      });
    return () => controller.abort();
  }, []);

  return (
    <div className="page">
      <header className="masthead">
        <Link className="wordmark" to="/">
          <span className="brand-symbol">
            <DexMark size={36} />
          </span>
          <span className="wordmark-text">
            <b>Dex</b>
            <span>Connectors</span>
          </span>
        </Link>
        <p className="lede">
          Open sourced and trusted connectors, take your process from prototype to production.
        </p>
        {catalog.status === "ready" ? <CatalogTotals connectors={catalog.value} /> : null}
      </header>
      <main>
        <Routes>
          <Route path="/" element={<CatalogPage catalog={catalog} />} />
          <Route path="/connectors/:id" element={<ConnectorPage catalog={catalog} />} />
        </Routes>
      </main>
    </div>
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

function CatalogPage({ catalog }: { catalog: LoadState<CatalogConnector[]> }) {
  const [query, setQuery] = useState("");
  if (catalog.status === "loading") {
    return <p className="status">Loading catalog…</p>;
  }
  if (catalog.status === "failed") {
    return <p className="status failed">{catalog.message}</p>;
  }
  const matched = catalog.value.filter((connector) => connectorMatchesQuery(connector, query));
  const groups = groupCatalog(matched);
  return (
    <div className="companies">
      <form className="search" role="search" onSubmit={(event) => event.preventDefault()}>
        <label htmlFor="connector-search">Search</label>
        <input
          id="connector-search"
          type="search"
          value={query}
          placeholder="Try channelPicker, send, messageReceived, or Gmail"
          onChange={(event) => setQuery(event.target.value)}
        />
      </form>
      {groups.length === 0 ? <p className="status">No connector matches that search.</p> : null}
      {groups.map((group) => (
        <section key={group.company} className="company">
          <div className="company-heading">
            <img src={companyLogoUrl(group.companyDirectory, logoOrigin)} alt="" width={56} height={56} />
            <h2>{group.company}</h2>
          </div>
          <ul className="cards">
            {group.connectors.map((connector) => (
              <li key={connector.id}>
                <Link className="card" to={`/connectors/${connector.id}`}>
                  <span className="card-title">
                    <span>{connector.name}</span>
                    <span className="version">{connector.version}</span>
                  </span>
                  <span className="description">{connector.description}</span>
                  <CapabilityChips connector={connector} />
                </Link>
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}

function CapabilityChips({ connector }: { connector: CatalogConnector }) {
  const chips = [
    ...connector.uiUnits.map((capability) => ({ ...capability, key: `ui-unit-${capability.name}` })),
    ...connector.triggers.map((capability) => ({ ...capability, key: `trigger-${capability.name}` })),
    ...connector.operations.map((capability) => ({ ...capability, key: `operation-${capability.name}` })),
  ];
  if (chips.length === 0) {
    return null;
  }
  return (
    <span className="chips">
      {chips.map((capability) => (
        <span key={capability.key} className={`kind kind-${capability.kind}`}>
          {capability.name}
        </span>
      ))}
    </span>
  );
}

function ConnectorPage({ catalog }: { catalog: LoadState<CatalogConnector[]> }) {
  const { id } = useParams();
  const connector =
    catalog.status === "ready" ? catalog.value.find((item) => item.id === id) : undefined;

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

  return (
    <article className="detail">
      <p className="back">
        <Link to="/">All connectors</Link>
      </p>
      <div className="detail-heading">
        <img src={companyLogoUrl(connector.companyDirectory, logoOrigin)} alt="" width={64} height={64} />
        <div>
          <p className="company-name">{connector.company}</p>
          <h2>{connector.name}</h2>
          <p className="version-line">
            {connector.version}
            <span>{connector.directory}</span>
          </p>
        </div>
      </div>
      <p className="description detail-description">{connector.description}</p>
      <CapabilityList title="UI units" capabilities={connector.uiUnits} />
      <CapabilityList title="Triggers" capabilities={connector.triggers} />
      <CapabilityList title="Operations" capabilities={connector.operations} />
    </article>
  );
}

function CapabilityList({ title, capabilities }: { title: string; capabilities: CatalogCapability[] }) {
  if (capabilities.length === 0) {
    return null;
  }
  return (
    <section>
      <h3>{title}</h3>
      <ul className="operations">
        {capabilities.map((capability) => (
          <li key={`${capability.kind}-${capability.name}`}>
            <span className={`kind kind-${capability.kind}`}>{capabilityKindLabel(capability.kind)}</span>
            <span>
              <strong>{capability.name}</strong>
              <span className="description">{capability.description}</span>
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
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
