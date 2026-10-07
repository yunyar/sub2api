# Payment and Client IP Provenance

## Verified topology

- Sub2API runs at `172.21.0.4` on its Docker network; its gateway is
  `172.21.0.1`.
- Sub2API port `8080` is published on `0.0.0.0`. Keep this mapping: users
  intentionally access the site directly by IP and port as well as by domain.
- OpenResty uses host networking and proxies the domain to
  `http://127.0.0.1:8080`.
- OpenResty sets `X-Real-IP` to `$remote_addr` and appends the incoming
  `X-Forwarded-For` chain using `$proxy_add_x_forwarded_for`.
- The verified Sub2API-side peer for the OpenResty-to-container connection is
  `172.21.0.1`.

## Application behavior

Payment orders, usage records, session-derived routing keys, security/audit
records, CAPTCHA/passkey checks, operational error records, access logs, and
IP-based rate limiting use Gin's trusted client-IP resolver. It uses forwarding
headers only when the immediate connection peer matches `server.trusted_proxies`;
otherwise it records the socket peer and ignores client-supplied
`X-Forwarded-For`, `X-Real-IP`, and `CF-Connecting-IP`.

The legacy forwarded-header compatibility preference remains available for
legacy resolver consumers. It does not override the trusted resolver used for
the records and security evidence listed above. Disabling the preference is
still recommended so API-key IP restrictions also use the configured proxy
chain.

## Recommended configuration

After verifying the source IP observed by Sub2API for both ingress paths, set
only the exact OpenResty peer:

```yaml
server:
  trusted_proxies:
    - 172.21.0.1/32
```

The equivalent environment setting is:

```text
SERVER_TRUSTED_PROXIES=172.21.0.1/32
```

Do not trust the full Docker bridge subnet, all private ranges, or
`0.0.0.0/0`. Turn off
`security.trust_forwarded_ip_for_api_key_acl` in the effective security
settings after configuring the trusted proxy. This preference may be stored in
the database, so verify the active admin security setting rather than assuming
that changing YAML alone overrides it.

Do not remove the public `0.0.0.0:8080` mapping or bind it to localhost as part
of this change. Direct IP-and-port access must remain available.

## Required source-IP validation

Before enabling the `/32` trust entry, send harmless, non-billing requests from
a known client through both the domain and direct `http://<server-ip>:8080`
paths. `GET /api/v1/payment/config` is a non-billing route suitable for this
check. Include deliberately conflicting `X-Forwarded-For`, `X-Real-IP`, and
`CF-Connecting-IP` values, then inspect the internal access records' `peer_ip`
(TCP source) and `client_ip` (trusted resolution):

- Domain requests must resolve to the actual client address observed by
  OpenResty, not a client-supplied prefix in XFF.
- Direct port-8080 requests must resolve to the direct caller's source address;
  forged forwarding headers must not change it.
- Confirm Sub2API sees `172.21.0.1` as the immediate peer only for the
  OpenResty upstream path. Confirm direct callers do not arrive with that same
  peer address due to Docker source NAT.

If direct requests also arrive from `172.21.0.1`, trusting that address would
make direct clients appear to be the proxy and could let them influence the
forwarded chain. In that case, preserve direct access but do not enable the
`/32` trust entry until the proxy and direct paths have distinguishable,
verified source addresses. Do not solve this by broadening the trusted range.

OpenResty must continue setting `X-Real-IP` from its verified `$remote_addr` and
appending the actual connecting address to XFF. If another upstream proxy or
CDN is introduced, validate its real-IP configuration and add only the
specific, directly connected proxy addresses to `server.trusted_proxies`.
