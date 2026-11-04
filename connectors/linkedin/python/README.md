# LinkedIn Python connector

The `getAuthenticatedProfile` Query reads bounded OpenID Connect UserInfo claims and requires a verified email. Install `dex-connector-linkedin==0.1.0`. Project connections use generated OAuth refresh and credential wiring. The runnable example is in `examples/authenticated_profile`.

Run `uv run --with-editable ../../../sdkpython pytest` from this directory. The example uses the generated OAuth project connection and a static connection name. Live LinkedIn authorization and UserInfo behavior have not been verified.
