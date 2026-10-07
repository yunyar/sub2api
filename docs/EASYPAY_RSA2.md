# EasyPay RSA2 configuration

Sub2API supports the existing EasyPay MD5 configuration and an optional RSA2 verification mode. New EasyPay instances recommend RSA2; an existing instance with no `gatewaySignType` remains on MD5 when edited.

## Sub2API fields

Keep the existing `pid`, `pkey`, gateway URL, and callback settings. For RSA2, configure:

| Config key | Value |
| --- | --- |
| `gatewaySignType` | `RSA2` |
| `gatewayPublicKey` | PEM-encoded RSA public key supplied by PayPro |
| `gatewayKeyId` | The Key ID paired with that public key |

`pkey` remains the EasyPay credential for order creation and query requests. It is not an RSA private key and stays masked in the admin form. Sub2API must never be given PayPro's RSA private key. The form checks PEM structure and Key ID syntax; server-side validation remains authoritative for parsing the RSA key and checking its size.

For legacy integrations, `gatewaySignType: MD5` keeps the existing behavior. Do not change the mode of an instance that still has pending orders; finish or drain those orders before rotating provider identity settings.

## Verification boundary

RSA2 proves that a signed message was produced by the holder of the PayPro private key. It does not prove that a customer payment was received. Successful callbacks must still be reconciled against a genuine gateway order query and the expected order identity, amount, and transaction ID before crediting an account. Browser return parameters are not payment evidence.
