# Shixiseng connector

`ListIndustries` returns the public industry names used for internship search filters. It does not fetch applicants, résumés, or job details.

Follow the [merchant access guide](https://open.shixiseng.com/) and contact Shixiseng business onboarding for APP_ID, APP_SECRET, and IP allowlisting. Store both credentials in a Dex Web Connection named `shixiseng`. The connector signs each GET request with uppercase MD5 over APP_ID, APP_SECRET, and a Unix-seconds timestamp, and sends Base64 APP_ID:timestamp in the Authorization header. The signature is kept out of returned values and errors.

The official API document defines [GET /industry/list](https://open.shixiseng.com/) and its `industry_list` response. Its example values date to 2016; live merchant access, current taxonomy, and IP allowlisting remain unverified. The [runnable Flow](go/examples/list-industries/) uses a real Dex Worker with a default mock.
