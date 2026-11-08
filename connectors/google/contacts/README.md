# Google Contacts connector

The Go and Python connectors list, get, and create contacts through the [Google People API](https://developers.google.com/people/api/rest). Their runnable [Go list](go/examples/contacts/README.md), [Go creation](go/examples/creation/README.md), [Python list](python/examples/contacts/README.md), and [Python creation](python/examples/creation/README.md) Flows use the static `google-contacts` Dex Web connection.

## Connection setup

In [Google Cloud Console](https://console.cloud.google.com/apis/credentials), select a project and enable **People API** under **APIs & Services → Library**. Configure OAuth consent and add the `contacts` scope under **Data Access**. Add test users while an External app remains in Testing. Create a Web application under **Clients**, register the exact redirect URI displayed by Dex Web, and save its Client ID and Client Secret in the connection. Authorize a Google account with offline access. Tokens remain Dex secrets.

`listContacts` uses a fixed `names,emailAddresses` field mask and returns one page of projected contacts with an optional next page token. Pass a returned `people/{id}` resource name to `getContact`. The page size defaults to 100 and can be set from 1 through 1000. Both operations return only the first display name and email address; either can be blank. The API origin, timeout, and response limit have manifest defaults; override them only for a trusted gateway or a known operational need.

The [connections.list](https://developers.google.com/people/api/rest/v1/people.connections/list) and [people.get](https://developers.google.com/people/api/rest/v1/people/get) methods define these read operations. Live Google authorization and API behavior have not been verified in this checkout.

`createContact` requires a given name and accepts optional family name and email address. It sends one People API create request. Since Google does not offer a create-contact idempotency key, a durable single-dispatch fence prevents a second send after an ambiguous response. A confirmed 429 or 503 can retry. An `uncertain` result means the user should inspect Contacts before creating another. The [people.createContact](https://developers.google.com/people/api/rest/v1/people/createContact) method defines the write request.
