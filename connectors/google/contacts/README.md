# Google Contacts connector

The Go connector reads one page of contacts or one contact through the [Google People API](https://developers.google.com/people/api/rest). Its [runnable contacts Flow](go/examples/contacts/README.md) uses the static `google-contacts` Dex Web connection and the `listContacts` Step.

## Connection setup

In [Google Cloud Console](https://console.cloud.google.com/apis/credentials), select a project and enable **People API** under **APIs & Services → Library**. Configure OAuth consent and add the `contacts` scope under **Data Access**. Add test users while an External app remains in Testing. Create a Web application under **Clients**, register the exact redirect URI displayed by Dex Web, and save its Client ID and Client Secret in the connection. Authorize a Google account with offline access. Tokens remain Dex secrets.

`listContacts` uses a fixed `names,emailAddresses` field mask and returns one page of projected contacts with an optional next page token. Pass a returned `people/{id}` resource name to `getContact`. The page size defaults to 100 and can be set from 1 through 1000. Both operations return only the first display name and email address; either can be blank. The API origin, timeout, and response limit have manifest defaults; override them only for a trusted gateway or a known operational need.

The [connections.list](https://developers.google.com/people/api/rest/v1/people.connections/list) and [people.get](https://developers.google.com/people/api/rest/v1/people/get) methods define these read operations. Live Google authorization and API behavior have not been verified in this checkout.
