export function WhatIsDexConnectors() {
  return (
    <article className="explainer">
      <header className="explainer-intro">
        <p className="eyebrow">How it works</p>
        <h1>One connector. Three reusable boundaries.</h1>
        <p>
          Dex Connectors link durable applications to products across the internet. Provider events
          enter Dex through typed Triggers, Flows call provider APIs through reusable Steps, and Dex
          Web composes connector-owned controls for setup and configuration.
        </p>
      </header>

      <section className="connector-map" aria-label="How products, connectors, and Dex applications interact">
        <section className="map-lane map-lane-events" aria-labelledby="events-lane-title">
          <h2 id="events-lane-title"><span>01</span> Events into Dex</h2>
          <div className="lane-grid">
            <div className="map-card endpoint-card">
              <span className="card-kicker">Product</span>
              <strong>Provider event</strong>
              <p>Webhook, subscription, or event source</p>
            </div>
            <RouteArrow label="event" />
            <div className="map-card capability-card trigger-card">
              <span className="capability-icon event-icon" aria-hidden="true"><i /></span>
              <span>
                <span className="card-kicker">Connector Trigger</span>
                <strong>Receive provider events</strong>
                <p>Stable event identity · typed payload · at-least-once delivery</p>
              </span>
            </div>
            <RouteArrow label={"start flow\ninvoke rpc"} />
            <div className="map-card endpoint-card target-card">
              <span className="card-kicker">Dex</span>
              <div className="target-choice"><i aria-hidden="true">▶</i><strong>Start a Flow</strong></div>
              <div className="target-choice"><i aria-hidden="true">↔</i><strong>Invoke Flow RPC</strong></div>
            </div>
          </div>
        </section>

        <section className="map-lane map-lane-operations" aria-labelledby="operations-lane-title">
          <h2 id="operations-lane-title"><span>02</span> Provider calls from Dex</h2>
          <div className="lane-grid">
            <div className="map-card endpoint-card flow-card">
              <span className="card-kicker">Dex Flow</span>
              <strong>Composed Steps</strong>
              <p>Use provider capabilities in any Flow</p>
              <div className="state-chips"><span>Attribute</span><span>Stream</span></div>
            </div>
            <RouteArrow label="step input" />
            <div className="map-card capability-card operations-card">
              <span className="card-kicker">Connector Steps</span>
              <div className="operation-pair">
                <span><b>Query</b><small>read</small></span>
                <span><b>Mutation</b><small>write</small></span>
              </div>
              <p>Timeout · retry · idempotency · explicit branches</p>
            </div>
            <RouteArrow label="provider call" />
            <div className="map-card endpoint-card">
              <span className="card-kicker">Product</span>
              <strong>Provider API</strong>
              <p>Read or change external state</p>
            </div>
          </div>
        </section>

        <section className="map-lane map-lane-configuration" aria-labelledby="configuration-lane-title">
          <h2 id="configuration-lane-title"><span>03</span> Configuration in Dex Web</h2>
          <div className="lane-grid">
            <div className="map-card endpoint-card">
              <span className="card-kicker">Product</span>
              <strong>Authorization</strong>
              <p>Provider account and permissions</p>
            </div>
            <RouteArrow label="credentials" />
            <div className="map-card capability-card configuration-card">
              <div>
                <span className="card-kicker">Connection runtime</span>
                <strong>Holds credentials</strong>
              </div>
              <span className="credential-boundary"><i aria-hidden="true" /> isolated boundary</span>
              <div>
                <span className="card-kicker">Configuration UI units</span>
                <strong>Scoped, non-secret values</strong>
              </div>
            </div>
            <RouteArrow label="ui config" />
            <div className="map-card endpoint-card web-card">
              <span className="browser-icon" aria-hidden="true"><i /><i /><i /></span>
              <span>
                <span className="card-kicker">Dex Web</span>
                <strong>Composes setup controls</strong>
                <p>Reusable units adapt to each Flow use</p>
              </span>
            </div>
          </div>
        </section>

        <p className="security-note">
          <span className="lock-icon" aria-hidden="true"><i /></span>
          Provider credentials stay in the Connector runtime—outside Flow input, state, results,
          receipts, Streams, logs, artifacts, and UI units.
        </p>
      </section>
    </article>
  );
}

function RouteArrow({ label }: { label: string }) {
  return (
    <div className="route-arrow" aria-hidden="true">
      <span>→</span>
      <small>{label}</small>
    </div>
  );
}
