# Payment verification and rollout

Date: 2026-10-07

## Trust boundary

The browser is never a payment authority. Checkout query parameters, redirect
results, screenshots and client-reported success do not authorize a balance
change. Payment credentials remain on the two servers.

EasyPay completion must be confirmed by a server-to-server order lookup using
the original local order and provider configuration. Confirmation compares the
merchant, external order identifier, positive exact-cent payment amount, final
payment status and nonempty gateway transaction identifier. The amount to
compare is `pay_amount`, not the balance credit amount after exchange rates or
bonuses.

Callbacks, authenticated verification, scheduled reconciliation, administrative
retry and direct fulfillment must all preserve this requirement. A provider
timeout, an unavailable provider, a missing order or incomplete identity is not
proof of payment. Leave the order uncredited and retry or investigate.

## Signature-confusion repair

- `CanonicalizeReturnURL` discards all caller-supplied query parameters and
  fragments. Return URLs are rebuilt using only server-generated result fields.
- Callback parameters use a strict allowlist. Unknown keys, including empty
  values and order-creation keys such as `return_url`, are rejected before
  signature verification. Duplicate keys are rejected.
- Standard MD5 canonicalization is not silently changed, because it is part of
  the legacy gateway protocol. New RSA2 configurations use escaped fields and
  separate signing purposes for notifications and query responses.
- RSA2 verification binds the expected key identifier, merchant, timestamp and
  nonce. Query responses must echo the freshly generated query nonce.
- A successful legacy query must include merchant and external order identity;
  contradictory textual/numeric payment statuses are rejected.
- Gateway HTTP redirects are not followed, preventing credential-bearing
  requests from being forwarded to a different destination. Oversized gateway
  responses and callback bodies are rejected rather than truncated.
- Callback failure logs contain a digest and length, not the complete signed
  payment payload.

Use HTTPS for the gateway endpoint. The custom PayPro RSA2 extension is not the
Alipay or WeChat Pay official API and does not turn OCR into cryptographically
verified bank settlement evidence.

## Persistent public-IP policy

`payment_risk_user_ips`, `payment_risk_ips` and `payment_risk_events` persist
account associations, active/released IP policies and their audit events.
Restarting the service does not clear these records. There is no automatic
expiry for a confirmed IP policy.

- Capture the recharge origin only from the configured trusted-proxy chain.
  The gateway callback sender is not the customer's IP.
- Exclude private, loopback, link-local, carrier-internal, multicast, reserved,
  documentation and other special-purpose addresses. Normalize IPv4-mapped
  addresses. If a trusted proxy has no attributable forwarded client address,
  do not treat the proxy itself as a customer.
- Associate only authenticated payment attempts and successful registration or
  login with accounts. A failed login with an arbitrary email is not evidence
  that the named account used that IP.
- Confirmed IP policies disable linked non-administrator accounts, check new
  registration and successful authentication, and run a periodic enforcement
  sweep every 60 seconds. Administrator account status is not automatically
  disabled; use an unrestricted network for recovery if its current IP is
  restricted.
- Automatic permanent action requires a cryptographically verified RSA2 query
  with the matching merchant and local external order but a conflicting
  authoritative payment amount. It also requires a previously recorded trusted
  IP association. Legacy historical IP attribution requires administrator
  review. An invalid signature, caller-supplied mismatch, a missing order,
  unpaid status or a provider timeout cannot create a permanent IP policy.
- Administrators can explicitly confirm an incident using the persisted local
  order, inspect retained records, manually scan associations, and release an
  IP policy. Releasing an IP does not silently reactivate accounts.

The default sliding-window limit is ten successfully created checkout orders
per public IP per ten minutes. The next attempt disables the initiating account
for administrator review, but frequency alone does not add a permanent
shared-IP policy. Configure the maximum with
`PAYMENT_RECHARGE_IP_LIMIT_10M` (1–1000; default 10).

In-flight checkouts reserve slots atomically. Requests exceeding available
in-flight slots receive a temporary limit, not an account ban. Failed provider
calls release their reservations and do not accumulate account penalties.
OAuth redirects that have not yet created a checkout do not count.

Shared public IPs can belong to unrelated people behind carrier NAT, workplaces,
schools or VPN exits. The requested linked-account restriction can therefore
affect legitimate users. Review the stored evidence before manual confirmation
and retain an appeal process. These IP rules supplement authoritative payment
verification; they do not replace it.

## Production rollout prerequisites

These source changes and local tests are not evidence of production deployment.

1. Back up the application configuration and database; preserve all existing
   custom playground, billing, notification and receipt-recognition functions.
2. Verify the new PayPro order-query contract and RSA2 response signing before
   selecting RSA2 on Sub2API. Existing MD5 configurations are not automatically
   converted; configure the trusted gateway public key and key ID explicitly.
3. Inventory historical nonempty duplicate payment transaction identities
   before applying the unique-index migration. Investigate discrepancies;
   never delete financial history or adjust balances to make a migration pass.
4. Configure exact trusted reverse-proxy addresses. Do not broadly trust the
   Internet, all private networks or arbitrary forwarded headers. Preserve the
   intended direct IP-and-port access path.
5. Run a real low-value authorized payment after deployment. Verify the local
   order, gateway order, transaction ID, credited amount and audit trail.
6. Test duplicate notifications and repeated verification locally, not by
   sending forged production callbacks. They must not credit the balance twice.
7. Restart only the explicitly authorized application services. Do not reboot
   either server.

The observed Docker/OpenResty installation uses `172.21.0.1` for the local
reverse-proxy connection. Verify that topology again before configuring
`172.21.0.1/32`; it is not a universal deployment default.

## Local regression coverage

- Original creation-signature delimiter collision, including a variant with a
  transaction identifier, and replay of the complete checkout parameters.
- Valid MD5/RSA2 notifications and rejection of unknown empty fields.
- Query stripping, encoded query keys and server-result-field override attempts.
- Missing merchant/order/transaction identity, conflicting payment states,
  mismatched amounts and nonfinite amounts.
- Java-generated RSA2 fixtures verified by the Go implementation.
- Invalid signing purpose, key identifier, timestamp, nonce and downgrade.
- No following of HTTP 301/302/303/307/308 responses.
- Oversized callback/request-response rejection.

Re-run payment-service, provider, handler, migration and frontend tests after
integrating any additional account/IP policy changes. Test logs alone do not
prove the live gateway or production configuration is using the new code.
