# VariFlight DataWorks connector

`GetFlightStatus` queries up to 20 status records for a flight number and local departure date. The official [Flight Status Query API V3.0](https://dataworks.variflight.com/about-us/download-zone/flight-status-query-api-docs/) defines `GET https://open-al.variflight.com/api/flight`, the double-MD5 signature, bare-array success response and `error_code` failure response. The connector returns a small subset of flight status fields.

## Authorization

Establish a VariFlight DataWorks partner account from [Contact Us](https://dataworks.variflight.com/), obtain assigned `appid` and secret `appsecurity`, and request allowlisting for the Dex Worker outbound public IP. Save both values in Dex Web Connections. The connector signs the exact request parameters and does not log the query URL or token. Leave timeout and maximum response size blank for displayed defaults. Provider queries may be billed under the partner agreement; temporary failures have three retry attempts.

## Example and verification

The [runnable Flow](go/examples/flight-status/) starts with flight number and `YYYY-MM-DD` departure date and completes with typed `GetFlightStatusOutput`. Its default mock returns a scheduled flight. Live partner authorization, IP allowlisting, billing and status response variation remain unverified without a user-controlled Dex Web Connection.
