// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package io.superdurable.dexconnectors.sdk.providerhttp;

import java.net.InetAddress;
import java.net.UnknownHostException;
import java.util.ArrayList;
import java.util.List;

/** Checks resolved provider addresses against an {@link AddressPolicy}. */
final class AddressPolicyRules {
    private static final byte[][] PRIVATE_NETWORKS = {
        IpAddressLiterals.parse("10.0.0.0"), IpAddressLiterals.parse("172.16.0.0"),
        IpAddressLiterals.parse("192.168.0.0"), IpAddressLiterals.parse("fc00::"),
    };
    private static final int[] PRIVATE_PREFIXES = {8, 12, 16, 7};
    private static final byte[] SHARED_NETWORK = IpAddressLiterals.parse("100.64.0.0");
    private static final byte[] LINK_LOCAL_IPV4 = IpAddressLiterals.parse("169.254.0.0");
    private static final byte[] LINK_LOCAL_IPV6 = IpAddressLiterals.parse("fe80::");
    private static final byte[] MULTICAST_IPV4 = IpAddressLiterals.parse("224.0.0.0");
    private static final byte[] MULTICAST_IPV6 = IpAddressLiterals.parse("ff00::");
    private static final byte[] METADATA_IPV6 = IpAddressLiterals.parse("fd00:ec2::254");

    private AddressPolicyRules() {
    }

    static String refusalReason(final byte[] rawAddress, final String scheme, final AddressPolicy policy) {
        final byte[] address = IpAddressLiterals.canonical(rawAddress);
        boolean isPrivate = IpAddressLiterals.isLoopback(address);
        for (int index = 0; index < PRIVATE_NETWORKS.length; index++) {
            isPrivate |= IpAddressLiterals.isInNetwork(address, PRIVATE_NETWORKS[index], PRIVATE_PREFIXES[index]);
        }
        final boolean isLinkLocal = IpAddressLiterals.isInNetwork(address, LINK_LOCAL_IPV4, 16)
                || IpAddressLiterals.isInNetwork(address, LINK_LOCAL_IPV6, 10);
        if (isLinkLocal || java.util.Arrays.equals(address, METADATA_IPV6)) {
            return "link-local addresses, which include the cloud metadata address, are never allowed";
        }
        if (isUnspecified(address) || IpAddressLiterals.isInNetwork(address, MULTICAST_IPV4, 4)
                || IpAddressLiterals.isInNetwork(address, MULTICAST_IPV6, 8) || isBroadcast(address)) {
            return "only unicast addresses are allowed";
        }
        if (policy == AddressPolicy.PUBLIC_HTTPS && !"https".equals(scheme)) {
            return "plain http is not allowed; use https";
        }
        if (policy == AddressPolicy.PUBLIC_HTTPS
                && (isPrivate || IpAddressLiterals.isInNetwork(address, SHARED_NETWORK, 10))) {
            return "only public addresses are allowed";
        }
        if (policy == AddressPolicy.PRIVATE_NETWORK_HTTP && "http".equals(scheme) && !isPrivate) {
            return "plain http is allowed only when every resolved address is loopback or private; use https";
        }
        return "";
    }

    static List<InetAddress> resolveCheckedAddresses(final String host, final String scheme, final AddressPolicy policy)
            throws UnknownHostException {
        final List<byte[]> addresses = new ArrayList<>();
        final byte[] literal = IpAddressLiterals.parse(host);
        if (literal != null) {
            addresses.add(literal);
        } else {
            for (InetAddress answer : InetAddress.getAllByName(host)) {
                addresses.add(answer.getAddress());
            }
            if (addresses.isEmpty()) {
                throw new UnknownHostException("provider address resolution returned no addresses");
            }
        }
        final List<InetAddress> checked = new ArrayList<>();
        for (byte[] address : addresses) {
            final String reason = refusalReason(address, scheme, policy);
            if (!reason.isEmpty()) {
                throw new AddressRefusalException(reason);
            }
            checked.add(IpAddressLiterals.toInetAddress(IpAddressLiterals.canonical(address)));
        }
        return checked;
    }

    private static boolean isUnspecified(final byte[] address) {
        for (byte value : address) {
            if (value != 0) {
                return false;
            }
        }
        return true;
    }

    private static boolean isBroadcast(final byte[] address) {
        if (address.length != 4) {
            return false;
        }
        for (byte value : address) {
            if ((value & 0xff) != 0xff) {
                return false;
            }
        }
        return true;
    }
}
