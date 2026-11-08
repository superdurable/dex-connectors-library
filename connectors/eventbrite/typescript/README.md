# Eventbrite TypeScript connector

This package implements `listOrganizations`, `listEvents`, and `getAttendee` for Dex TypeScript Flows. All three operations replay the Go reference's provider requests, branches, retries, and exact outputs through the shared conformance scenarios.

Create an Eventbrite API key under Account Settings > Developer Links > API Keys and copy its **Private Token** into the Dex Web connection secret. The package sends it only as `Authorization: Bearer` and excludes attendee profiles, answers, barcodes, and email from Flow results. The [connector README](../README.md) describes operation inputs and defaults; [organization inspection](examples/organization-inspection/README.md) is the runnable example.

Live Eventbrite authorization and responses have not been verified locally. Package release is independent of the Go and Python releases.
