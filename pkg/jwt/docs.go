// Package jwt provides HS256 token generation and strict pre-signing and token
// validation of required issuer, audience, expiry, and temporal claims.
//
// Requiring the audience claim is not backwards compatible: access tokens signed
// before it was added carry no aud and are rejected. The blast radius is one
// access-token lifetime (Config.Expiry, 15 minutes by default) and it does not
// log anyone out — refresh tokens are opaque rows owned by the user context, not
// tokens from this package, so the 401 an old access token now gets is the same
// 401 an expired one gets and the client's normal refresh recovers from it. That
// is deliberately not softened into "validate aud only when present": a token
// this service will accept without knowing which API it was minted for is the
// hole the claim exists to close, and it would stay open for every token, not
// for fifteen minutes.
package jwt
