# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Inert deterministic RSA key shared by Go and Python replay tests."""


def synthetic_private_key() -> str:
    """Reconstruct the inert RSA key from the Go fixture's synthetic primes."""
    from cryptography.hazmat.primitives import serialization
    from cryptography.hazmat.primitives.asymmetric import rsa

    first = int(
        "d95674ae10520f2c3911db6026b0560bcb4905661ed758db2c9873b90b90346dc4d32d5817098a69fa8d6d7514b3f14093557b094157ef717161952a361250ed",
        16,
    )
    second = int(
        "d3083c06a05a7a1b98925fc3a98fdf424eb4fbf129ed86cb8061a2350abc262d9e25daca9088729976640e043a2a92885319470e2e1c9abf4b23eeee3a410c43",
        16,
    )
    exponent = 65537
    private_exponent = pow(exponent, -1, (first - 1) * (second - 1))
    numbers = rsa.RSAPrivateNumbers(
        first,
        second,
        private_exponent,
        private_exponent % (first - 1),
        private_exponent % (second - 1),
        pow(second, -1, first),
        rsa.RSAPublicNumbers(exponent, first * second),
    )
    return (
        numbers.private_key()
        .private_bytes(
            serialization.Encoding.PEM,
            serialization.PrivateFormat.TraditionalOpenSSL,
            serialization.NoEncryption(),
        )
        .decode()
    )
