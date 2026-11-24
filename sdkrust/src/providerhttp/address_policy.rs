// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

//! Provider address checks applied to every DNS answer before dialing.

use std::fmt;
use std::net::{IpAddr, Ipv4Addr, Ipv6Addr, SocketAddr};

/// Selects public HTTPS, private-network HTTP, or unrestricted dialing.
#[derive(Clone, Copy, Debug, Eq, Hash, PartialEq)]
pub enum AddressPolicy {
    /// Only HTTPS to public unicast addresses (`publicHTTPS`).
    PublicHttps,
    /// HTTPS anywhere unicast, and plain HTTP only when every address is loopback or private
    /// (`privateNetworkHTTP`).
    PrivateNetworkHttp,
    /// The caller owns network policy; no address check (`unrestricted`).
    Unrestricted,
}

impl AddressPolicy {
    /// Parses the manifest and sdkgo name, such as `publicHTTPS`.
    pub fn from_wire_name(value: &str) -> Option<Self> {
        match value {
            "publicHTTPS" => Some(AddressPolicy::PublicHttps),
            "privateNetworkHTTP" => Some(AddressPolicy::PrivateNetworkHttp),
            "unrestricted" => Some(AddressPolicy::Unrestricted),
            _ => None,
        }
    }

    /// Returns the manifest and sdkgo name.
    pub fn as_wire_name(self) -> &'static str {
        match self {
            AddressPolicy::PublicHttps => "publicHTTPS",
            AddressPolicy::PrivateNetworkHttp => "privateNetworkHTTP",
            AddressPolicy::Unrestricted => "unrestricted",
        }
    }
}

/// A policy refusal before dialing; its reason contains no host or credential.
#[derive(Clone, Debug, Eq, PartialEq)]
pub struct AddressRefusal {
    reason: String,
}

impl AddressRefusal {
    /// Creates a refusal with a host-free reason.
    pub fn new(reason: impl Into<String>) -> Self {
        Self {
            reason: reason.into(),
        }
    }

    /// Returns the host-free reason.
    pub fn reason(&self) -> &str {
        &self.reason
    }
}

impl fmt::Display for AddressRefusal {
    fn fmt(&self, formatter: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(formatter, "address not allowed: {}", self.reason)
    }
}

impl std::error::Error for AddressRefusal {}

fn unmapped(address: IpAddr) -> IpAddr {
    match address {
        IpAddr::V6(v6) => v6.to_ipv4_mapped().map_or(IpAddr::V6(v6), IpAddr::V4),
        IpAddr::V4(_) => address,
    }
}

fn is_private(address: IpAddr) -> bool {
    match address {
        IpAddr::V4(v4) => v4.is_loopback() || v4.is_private(),
        IpAddr::V6(v6) => v6.is_loopback() || (v6.segments()[0] & 0xfe00) == 0xfc00,
    }
}

fn is_link_local(address: IpAddr) -> bool {
    match address {
        IpAddr::V4(v4) => v4.is_link_local(),
        IpAddr::V6(v6) => {
            (v6.segments()[0] & 0xffc0) == 0xfe80
                || v6 == Ipv6Addr::new(0xfd00, 0xec2, 0, 0, 0, 0, 0, 0x254)
        }
    }
}

fn is_shared_address_space(address: IpAddr) -> bool {
    match address {
        IpAddr::V4(v4) => v4.octets()[0] == 100 && (v4.octets()[1] & 0xc0) == 64,
        IpAddr::V6(_) => false,
    }
}

/// Returns why `address` is refused for `scheme` under `policy`, or `None` when allowed.
///
/// IPv4-mapped IPv6 addresses are checked as IPv4. Link-local addresses, which include the
/// cloud metadata addresses, and non-unicast addresses are always refused under a restricted
/// policy; [`AddressPolicy::Unrestricted`] allows everything.
pub fn address_refusal_reason(
    address: IpAddr,
    scheme: &str,
    policy: AddressPolicy,
) -> Option<&'static str> {
    if policy == AddressPolicy::Unrestricted {
        return None;
    }
    let address = unmapped(address);
    if is_link_local(address) {
        return Some(
            "link-local addresses, which include the cloud metadata address, are never allowed",
        );
    }
    let is_non_unicast = match address {
        IpAddr::V4(v4) => v4.is_unspecified() || v4.is_multicast() || v4 == Ipv4Addr::BROADCAST,
        IpAddr::V6(v6) => v6.is_unspecified() || v6.is_multicast(),
    };
    if is_non_unicast {
        return Some("only unicast addresses are allowed");
    }
    let is_private_address = is_private(address);
    match policy {
        AddressPolicy::PublicHttps if scheme != "https" => {
            Some("plain http is not allowed; use https")
        }
        AddressPolicy::PublicHttps if is_private_address || is_shared_address_space(address) => {
            Some("only public addresses are allowed")
        }
        AddressPolicy::PrivateNetworkHttp if scheme == "http" && !is_private_address => Some(
            "plain http is allowed only when every resolved address is loopback or private; use https",
        ),
        _ => None,
    }
}

/// Checks every resolved address and returns the ones to dial, IPv4-mapped ones unmapped.
///
/// One refused address refuses the whole answer, so a mixed answer cannot reach a private host.
pub(crate) fn check_resolved_addresses(
    addresses: Vec<SocketAddr>,
    scheme: &str,
    policy: AddressPolicy,
) -> Result<Vec<SocketAddr>, AddressRefusal> {
    if policy == AddressPolicy::Unrestricted {
        return Ok(addresses);
    }
    let mut checked = Vec::with_capacity(addresses.len());
    for address in addresses {
        if let Some(reason) = address_refusal_reason(address.ip(), scheme, policy) {
            return Err(AddressRefusal::new(reason));
        }
        checked.push(SocketAddr::new(unmapped(address.ip()), address.port()));
    }
    Ok(checked)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn go_address_policy_table() {
        let table: &[(&[&str], bool, bool, bool)] = &[
            (&["127.0.0.1"], true, true, false),
            (&["::1"], true, true, false),
            (&["10.1.2.3"], true, true, false),
            (&["172.16.0.5"], true, true, false),
            (&["172.31.255.255"], true, true, false),
            (&["192.168.1.1"], true, true, false),
            (&["fc00::1"], true, true, false),
            (&["fd12:3456::1"], true, true, false),
            (&["10.0.0.1", "192.168.0.2", "::1"], true, true, false),
            (&["::ffff:10.0.0.1"], true, true, false),
            (&["::ffff:8.8.8.8"], false, true, true),
            (&["8.8.8.8"], false, true, true),
            (&["2001:4860:4860::8888"], false, true, true),
            (&["8.8.8.8", "1.1.1.1"], false, true, true),
            (&["100.64.0.1"], false, true, false),
            (&["10.0.0.1", "8.8.8.8"], false, true, false),
            (&["8.8.8.8", "10.0.0.1"], false, true, false),
            (&["169.254.169.254"], false, false, false),
            (&["169.254.0.1"], false, false, false),
            (&["fe80::1"], false, false, false),
            (&["febf::1"], false, false, false),
            (&["::ffff:169.254.169.254"], false, false, false),
            (&["fd00:ec2::254"], false, false, false),
            (&["127.0.0.1", "169.254.169.254"], false, false, false),
            (&["8.8.8.8", "169.254.169.254"], false, false, false),
            (&["0.0.0.0"], false, false, false),
            (&["::"], false, false, false),
            (&["224.0.0.1"], false, false, false),
            (&["ff02::1"], false, false, false),
            (&["255.255.255.255"], false, false, false),
        ];
        for (addresses, private_http, private_https, public_https) in table {
            let scenarios = [
                ("http", AddressPolicy::PrivateNetworkHttp, *private_http),
                ("https", AddressPolicy::PrivateNetworkHttp, *private_https),
                ("http", AddressPolicy::PublicHttps, false),
                ("https", AddressPolicy::PublicHttps, *public_https),
            ];
            for (scheme, policy, expected) in scenarios {
                let is_allowed = addresses.iter().all(|address| {
                    let parsed: IpAddr = address.parse().expect("valid test address");
                    address_refusal_reason(parsed, scheme, policy).is_none()
                });
                assert_eq!(is_allowed, expected, "{addresses:?} {scheme} {policy:?}");
            }
        }
    }
}
