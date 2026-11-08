# Odoo connector

The Go `searchPartners` Query calls Odoo 19's [JSON-2 `res.partner/search_read`](https://www.odoo.com/documentation/19.0/developer/reference/external_api.html) method once per Flow Step. It searches company partners by an Odoo `ilike` name pattern and returns only their numeric IDs and names. Python and TypeScript packages are pending.

Odoo's JSON-2 API requires an Odoo 19 or newer deployment and an eligible external API plan. In Dex Web Connections, enter the site's HTTPS origin as `baseUrl`, without `/json/2`; enter the exact database name as `database`. On Odoo Online this is usually the subdomain before `.odoo.com`; an administrator can confirm it on other deployments. Generate a secret `apiKey` under your user avatar > **My Profile or Preferences > Account Security > New API Key**, choose a description and expiry, and copy it once. A dedicated read-only user limits access. Leave `timeout` and `maxResponseBytes` blank for the displayed defaults.

Pass `namePattern` using Odoo `ilike` wildcards, such as `%Acme%`, and `limit` from 1 to 100. An empty match is a successful empty page. The [Go example](go/examples/partner-search/README.md) uses the static `odoo` connection and real Dex fixture integration. Live Odoo authorization and search have not been verified.
