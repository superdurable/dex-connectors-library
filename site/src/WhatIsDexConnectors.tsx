// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

import type { ReactNode } from "react";

import { Icon, type IconName } from "./Icons";

export function WhatIsDexConnectors() {
  return (
    <article className="explainer">
      <h1 className="explainer-title">A connector plugs one product into Dex</h1>

      <section className="connector-map" aria-label="How products, connectors, and Dex applications interact">
        <section className="map-lane map-lane-events" aria-labelledby="events-lane-title">
          <h2 id="events-lane-title">Events in</h2>
          <div className="lane-grid">
            <MapEndpoint icon="webhook" kicker="Product" title="Provider event">
              <p>Webhook, subscription, or polled source</p>
            </MapEndpoint>
            <MapLink />
            <div className="map-connector">
              <div className="connector-heading">
                <ConnectorIcon name="bolt" />
                <span>
                  <span className="card-kicker">Connector</span>
                  <strong>Trigger</strong>
                </span>
              </div>
              <ul className="connector-traits">
                <li>Stable event identity</li>
                <li>Typed payload</li>
                <li>At-least-once delivery</li>
              </ul>
            </div>
            <MapLink />
            <MapEndpoint icon="workflow" kicker="Dex">
              <ul className="endpoint-choices">
                <li><Icon name="play" size={14} />Start a Flow</li>
                <li><Icon name="arrowsLeftRight" size={14} />Invoke Flow RPC</li>
              </ul>
            </MapEndpoint>
          </div>
        </section>

        <section className="map-lane map-lane-operations" aria-labelledby="operations-lane-title">
          <h2 id="operations-lane-title">API calls out</h2>
          <div className="lane-grid">
            <MapEndpoint icon="workflow" kicker="Dex" title="Flow" />
            <MapLink />
            <div className="map-connector">
              <span className="card-kicker">Connector</span>
              <strong className="connector-title">Steps</strong>
              <ul className="operation-pair">
                <li>
                  <ConnectorIcon name="database" />
                  <span><b>Query</b><small>read</small></span>
                </li>
                <li>
                  <ConnectorIcon name="pencil" />
                  <span><b>Mutation</b><small>write</small></span>
                </li>
              </ul>
              <ul className="connector-traits">
                <li>Timeout</li>
                <li>Retry</li>
                <li>Idempotency</li>
                <li>Explicit branches</li>
              </ul>
            </div>
            <MapLink />
            <MapEndpoint icon="globe" kicker="Product" title="Provider API" />
          </div>
        </section>

        <section className="map-lane map-lane-configuration" aria-labelledby="configuration-lane-title">
          <h2 id="configuration-lane-title">Setup in Dex Web</h2>
          <div className="lane-grid">
            <MapEndpoint icon="key" kicker="Product" title="Authorization">
              <p>Provider account and granted scopes</p>
            </MapEndpoint>
            <MapLink />
            <div className="map-connector">
              <span className="card-kicker">Connector</span>
              <div className="connector-part">
                <ConnectorIcon name="lock" />
                <span><strong>Connection runtime</strong><small>Credentials never leave it</small></span>
              </div>
              <div className="credential-boundary">
                <span><Icon name="shield" size={13} />credential boundary</span>
              </div>
              <div className="connector-part">
                <ConnectorIcon name="sliders" />
                <span><strong>Configuration UI units</strong><small>Scoped, non-secret values</small></span>
              </div>
            </div>
            <MapLink />
            <MapEndpoint icon="browser" kicker="Dex Web" title="Forms for each Flow" />
          </div>
        </section>
      </section>
    </article>
  );
}

function MapEndpoint({ icon, kicker, title, children }: {
  icon: IconName;
  kicker: string;
  title?: string;
  children?: ReactNode;
}) {
  return (
    <div className="map-endpoint">
      <span className="endpoint-icon"><Icon name={icon} size={17} /></span>
      <div>
        <span className="card-kicker">{kicker}</span>
        {title ? <strong>{title}</strong> : null}
        {children}
      </div>
    </div>
  );
}

function ConnectorIcon({ name }: { name: IconName }) {
  return <span className="connector-icon"><Icon name={name} size={18} /></span>;
}

function MapLink() {
  return (
    <div className="map-link" aria-hidden="true">
      <span className="map-link-line" />
      <Icon name="chevronRight" size={14} />
    </div>
  );
}
