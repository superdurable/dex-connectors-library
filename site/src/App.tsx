import { useEffect, useMemo, useState } from "react";
import { Link, Route, Routes, useParams } from "react-router-dom";
import {
  catalogDocumentUrl,
  catalogTotals,
  companyDirectory,
  companyLogoUrl,
  connectorMatchesQuery,
  groupCatalog,
  readCatalog,
  type CatalogConnector,
} from "./catalogModel.mjs";
import { DexMark } from "./DexMark";

export function App() {
  const catalog = useCatalog();
  return (
    <Routes>
      <Route path="/" element={<Directory catalog={catalog} />} />
      <Route path="/connectors/:connectorId" element={<DetailRoute catalog={catalog} />} />
    </Routes>
  );
}

function DetailRoute({ catalog }: { catalog: CatalogState }) {
  const { connectorId } = useParams();
  return <ConnectorDetail catalog={catalog} connectorId={connectorId ?? ""} />;
}

function Directory({ catalog }: { catalog: CatalogState }) {
  const [query, setQuery] = useState("");
  const visible = useMemo(() => {
    if (catalog.status !== "ready") {
      return [];
    }
    return catalog.connectors.filter((connector) => connectorMatchesQuery(connector, query));
  }, [catalog, query]);
  const totals = catalog.status === "ready" ? catalogTotals(catalog.connectors) : null;
  return (
    <main className="page">
      <header className="masthead">
        <Link className="wordmark" to="/">
          <DexMark size={36} />
          <span>Dex</span>
          <span className="wordmark-rest">Connectors</span>
        </Link>
        {totals ? (
          <p className="totals">
            <strong>{totals.companies}</strong> companies
            <strong>{totals.connectors}</strong> connectors
            <strong>{totals.operations}</strong> operations
            <strong>{totals.triggers}</strong> triggers
          </p>
        ) : null}
      </header>
      <p className="lede">Take a process from a prototype to production with open sourced and trusted connectors.</p>
      <label className="search" htmlFor="connector-search">
        Search
        <input
          id="connector-search"
          value={query}
          placeholder="Try send, messageReceived, or Gmail"
          onChange={(event) => setQuery(event.target.value)}
        />
      </label>
      {catalog.status === "error" ? <p className="status">{catalog.message}</p> : null}
      {catalog.status === "loading" ? <p className="status">Loading the catalog.</p> : null}
      <div className="groups">
        {groupCatalog(visible).map((group) => (
          <section className="group" key={group.companyDirectory}>
            <h2>
              <img src={companyLogoUrl(group.companyDirectory, logoSource())} alt="" />
              {group.company}
            </h2>
            <div className="cards">
              {group.connectors.map((connector) => (
                <Link className="card" key={connector.id} to={`/connectors/${connector.id}`}>
                  <h3>{connector.name}</h3>
                  <p>{connector.description}</p>
                  <CapabilityList connector={connector} />
                </Link>
              ))}
            </div>
          </section>
        ))}
      </div>
    </main>
  );
}

function ConnectorDetail({ catalog, connectorId }: { catalog: CatalogState; connectorId: string }) {
  const connector = catalog.status === "ready"
    ? catalog.connectors.find((item) => item.id === connectorId)
    : undefined;
  return (
    <main className="page">
      <header className="masthead">
        <Link className="wordmark" to="/">
          <DexMark size={36} />
          <span>Dex</span>
          <span className="wordmark-rest">Connectors</span>
        </Link>
      </header>
      {catalog.status === "loading" ? <p className="status">Loading the catalog.</p> : null}
      {catalog.status === "error" ? <p className="status">{catalog.message}</p> : null}
      {catalog.status === "ready" && !connector ? <p className="status">That connector is not in the catalog.</p> : null}
      {connector ? (
        <article className="detail">
          <img src={companyLogoUrl(companyDirectory(connector.directory), logoSource())} alt="" />
          <h1>{connector.name}</h1>
          <p>{connector.description}</p>
          <p className="meta">{connector.company} · {connector.version}</p>
          <h2>Triggers</h2>
          <CapabilityList connector={connector} only="trigger" />
          <h2>Operations</h2>
          <CapabilityList connector={connector} only="operation" />
        </article>
      ) : null}
    </main>
  );
}

function CapabilityList({ connector, only }: { connector: CatalogConnector; only?: "trigger" | "operation" }) {
  const capabilities = only === "trigger"
    ? connector.triggers
    : only === "operation"
      ? connector.operations
      : [...connector.triggers, ...connector.operations];
  if (capabilities.length === 0) {
    return <p className="empty">None published.</p>;
  }
  return (
    <ul className="chips">
      {capabilities.map((capability) => (
        <li className={`chip kind-${capability.kind || "trigger"}`} key={`${capability.kind}-${capability.name}`}>
          {capability.name}
        </li>
      ))}
    </ul>
  );
}

type CatalogState =
  | { status: "loading" }
  | { status: "ready"; connectors: CatalogConnector[] }
  | { status: "error"; message: string };

function useCatalog(): CatalogState {
  const [state, setState] = useState<CatalogState>({ status: "loading" });
  useEffect(() => {
    const controller = new AbortController();
    fetch(catalogDocumentUrl(), { signal: controller.signal })
      .then(async (response) => {
        if (!response.ok) {
          throw new Error(`catalog request failed: ${response.status}`);
        }
        setState({ status: "ready", connectors: readCatalog(await response.text()) });
      })
      .catch((error: unknown) => {
        if (controller.signal.aborted) {
          return;
        }
        setState({ status: "error", message: error instanceof Error ? error.message : "catalog request failed" });
      });
    return () => controller.abort();
  }, []);
  return state;
}

function logoSource(): "local" | "published" {
  return import.meta.env.DEV ? "local" : "published";
}
