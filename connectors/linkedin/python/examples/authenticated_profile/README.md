# LinkedIn authenticated profile Python example

Create a Dex Web connection named `linkedin-signup`. Configure a LinkedIn developer application at https://www.linkedin.com/developers/apps with the Sign in with LinkedIn using OpenID Connect product, its redirect URI, and `openid profile email` scopes. Set `DEX_FLOW_SERVICE_ADDRESS` to a running `dexcli dev` Flow Service and run `python -m examples.authenticated_profile.main` from `connectors/linkedin/python`. Start `LinkedInAuthenticatedProfilePython` with `{}`. The Flow stores the verified profile and completes. Optional failure branches remain unwired and fail the Flow.

The integration test runs a real Dex Worker and Client against `dexcli dev` with a local HTTP provider stand-in. Live LinkedIn authorization and UserInfo behavior remain unverified.
