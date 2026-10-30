// Copyright (c) 2026 Super Durable
// SPDX-License-Identifier: MIT

package snowflake

import (
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"time"
	"unicode"

	"github.com/superdurable/dex-connectors-library/sdkgo"
	"github.com/superdurable/dex-connectors-library/sdkgo/oauthtoken"
	"github.com/superdurable/dex-connectors-library/sdkgo/providerhttp"
)

const (
	keyPairJWTTokenType              = "KEYPAIR_JWT"
	programmaticAccessTokenTokenType = "PROGRAMMATIC_ACCESS_TOKEN"
	// keyPairJWTLifetime stays under Snowflake's one-hour JWT limit, as Snowflake's own sample does.
	keyPairJWTLifetime = 59 * time.Minute
	// maximumUserNameBytes bounds the key-pair user name copied into JWT claims.
	maximumUserNameBytes = 255
)

// requestAuthorization is the bearer credential and its X-Snowflake-Authorization-Token-Type for one request.
type requestAuthorization struct {
	token     sdkgo.SecretString
	tokenType string
}

// resolveAuthorization resolves the connection's credentials for one call and never returns key material in errors.
func (client *Client) resolveAuthorization(call sdkgo.Call) (requestAuthorization, error) {
	credentials, err := client.credentials.Resolve(call)
	if err != nil {
		return requestAuthorization{}, &credentialError{message: "Snowflake connection credentials are unavailable"}
	}
	switch credentials.AuthMethodID {
	case KeyPairAuthMethodID:
		token, err := signKeyPairJWT(client.jwtAccount, credentials.User, credentials.PrivateKey, client.now())
		if err != nil {
			return requestAuthorization{}, err
		}
		return requestAuthorization{token: token, tokenType: keyPairJWTTokenType}, nil
	case ProgrammaticAccessTokenAuthMethodID:
		if !providerhttp.IsHeaderSafeCredential(credentials.ProgrammaticAccessToken.Reveal()) {
			return requestAuthorization{}, &credentialError{
				message: "the Snowflake programmatic access token is blank or contains spaces or non-ASCII characters",
			}
		}
		return requestAuthorization{token: credentials.ProgrammaticAccessToken, tokenType: programmaticAccessTokenTokenType}, nil
	default:
		return requestAuthorization{}, &credentialError{message: "the Snowflake connection selects no supported authentication method"}
	}
}

// signKeyPairJWT builds Snowflake's RS256 key-pair JWT; oauthtoken's signer requires an aud claim Snowflake lacks.
func signKeyPairJWT(jwtAccount string, user string, privateKeyPEM sdkgo.SecretString, now time.Time) (sdkgo.SecretString, error) {
	if user == "" || strings.TrimSpace(user) != user || len(user) > maximumUserNameBytes ||
		strings.ContainsFunc(user, unicode.IsControl) || strings.ContainsRune(user, '"') {
		return sdkgo.SecretString{}, &credentialError{message: "the Snowflake key-pair user name is blank, too long, or contains quotes or control characters"}
	}
	privateKey, err := parseUnencryptedPrivateKey(privateKeyPEM.Reveal())
	if err != nil {
		return sdkgo.SecretString{}, err
	}
	fingerprint, err := publicKeyFingerprint(&privateKey.PublicKey)
	if err != nil {
		return sdkgo.SecretString{}, err
	}
	qualifiedUserName := jwtAccount + "." + strings.ToUpper(user)
	issuedAt := now.UTC()
	claims, err := json.Marshal(map[string]any{
		"iss": qualifiedUserName + "." + fingerprint,
		"sub": qualifiedUserName,
		"iat": issuedAt.Unix(),
		"exp": issuedAt.Add(keyPairJWTLifetime).Unix(),
	})
	if err != nil {
		return sdkgo.SecretString{}, &credentialError{message: "the Snowflake key-pair JWT claims could not be encoded"}
	}
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	unsigned := header + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	signature, err := rsa.SignPKCS1v15(rand.Reader, privateKey, crypto.SHA256, digest[:])
	if err != nil {
		return sdkgo.SecretString{}, &credentialError{message: "the Snowflake key-pair JWT could not be signed"}
	}
	return sdkgo.NewSecretString(unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)), nil
}

func parseUnencryptedPrivateKey(privateKeyPEM string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.TrimSpace(privateKeyPEM)))
	if block != nil && block.Type == "ENCRYPTED PRIVATE KEY" {
		return nil, &credentialError{
			message: "the Snowflake private key is encrypted; export an unencrypted PKCS #8 key with openssl pkcs8 -topk8 -nocrypt",
		}
	}
	privateKey, err := oauthtoken.ParseRSAPrivateKeyPEM(privateKeyPEM)
	if err != nil {
		return nil, &credentialError{message: "the Snowflake private key is not an unencrypted PEM RSA private key"}
	}
	return privateKey, nil
}

// publicKeyFingerprint is SHA256: and the base64 SHA-256 of the DER SubjectPublicKeyInfo, as DESC USER shows RSA_PUBLIC_KEY_FP.
func publicKeyFingerprint(publicKey *rsa.PublicKey) (string, error) {
	encoded, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return "", &credentialError{message: "the Snowflake public key fingerprint could not be computed"}
	}
	digest := sha256.Sum256(encoded)
	return "SHA256:" + base64.StdEncoding.EncodeToString(digest[:]), nil
}
