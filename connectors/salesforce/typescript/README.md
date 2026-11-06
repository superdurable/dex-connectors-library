# Salesforce connector for TypeScript Dex Flows

This package provides typed Salesforce query, record read, external ID upsert, and record update operations. OAuth and JWT bearer refresh use the connector manifest's generated declarations. Configure a Salesforce connection in Dex Web and use a static connection name in a Flow.

Live Salesforce authorization has not been verified for this port.

SOQL `dateTime` bindings accept Go RFC 3339 forms, including a single-digit hour and comma fraction, while rejecting impossible calendar dates. The connector accepts an instance URL according to Go's base URL validation and uses the host transport for the provider connection.
