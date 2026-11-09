# Gusto connector

The Go, [Python](python/README.md), and [TypeScript](typescript/README.md) packages implement `getEmployee` using Gusto's [employee endpoint](https://docs.gusto.com/embedded-payroll/reference/get-v1-employees). The Query accepts an employee UUID and returns selected identity fields: UUID, first and last name, optional email, and company UUID. It excludes compensation data. HTTP 404 selects optional `missing`; 429, 408, 5xx, and transport failures retry. Invalid responses and definitive rejections select separate optional branches.

Create an application in the [Gusto Developer Portal](https://dev.gusto.com/) and register the exact HTTPS callback shown by Dex Web. Authorize the intended company with `employees:read`. Configure the client ID, client secret, and callback URI in the Dex connection; Dex stores rotating access and refresh tokens. The connector uses the production API origin and sends Gusto API version `2026-06-15`.

The [Go](go/examples/employee/README.md), [Python](python/examples/employee/README.md), and [TypeScript](typescript/examples/employee/README.md) Flow examples use a static `gusto` connection. [Go-recorded conformance scenarios](../../conformance/connectors/gusto/scenarios.json) cover each branch and retry class. Live Gusto consent and provider behavior have not been verified in this checkout.
