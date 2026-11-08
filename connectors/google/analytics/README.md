# Google Analytics 4 connector

The Go connector runs a bounded GA4 report, reads property metadata, and lists one page of audiences. The [property Flow](go/examples/property/README.md) uses the static `google-analytics-4` Dex Web connection and the operation-specific `getProperty` Step.

## Connection setup

In [Google Cloud Console](https://console.cloud.google.com/apis/credentials), select a project and enable **Google Analytics Data API** and **Google Analytics Admin API** under **APIs & Services → Library**. Configure OAuth consent, add `analytics.readonly` under **Data Access**, and add test users while an External app remains in Testing. Create a Web application under **Clients**, register the redirect URI displayed by Dex Web, then save its Client ID and Client Secret in the connection. Authorize a Google account with Viewer or higher access to the relevant GA4 property. Dex requests offline access for token renewal.

Find the numeric Property ID in **Google Analytics → Admin → Property settings → Property ID** and pass it to each operation. `runReport` takes an inclusive `YYYY-MM-DD` range, up to ten dimension names, at least one metric name, and a row limit up to 10000. `listAudiences` returns a cursor for the next page. The API origins, timeout, and response limit have manifest defaults; only override them for a trusted gateway or known operational need. Tokens remain Dex secrets and never enter Flow input or results.

The [GA4 Data API report method](https://developers.google.com/analytics/devguides/reporting/data/v1/rest/v1beta/properties/runReport) and [Admin API property](https://developers.google.com/analytics/devguides/config/admin/v1/rest/v1beta/properties/get) and [audience](https://developers.google.com/analytics/devguides/config/admin/v1/rest/v1alpha/properties.audiences/list) methods define these operations. Provider authorization and live API behavior have not been verified in this checkout.
