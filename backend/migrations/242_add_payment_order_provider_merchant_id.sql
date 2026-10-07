ALTER TABLE payment_orders
ADD COLUMN IF NOT EXISTS provider_merchant_id VARCHAR(128);

ALTER TABLE payment_orders
ADD COLUMN IF NOT EXISTS provider_gateway_identity VARCHAR(64);

UPDATE payment_orders
SET provider_key = NULLIF(BTRIM(provider_snapshot->>'provider_key'), '')
WHERE NULLIF(BTRIM(provider_key), '') IS NULL
  AND provider_snapshot IS NOT NULL
  AND NULLIF(BTRIM(provider_snapshot->>'provider_key'), '') IS NOT NULL;

UPDATE payment_orders
SET provider_merchant_id = NULLIF(BTRIM(provider_snapshot->>'merchant_id'), '')
WHERE NULLIF(BTRIM(provider_merchant_id), '') IS NULL
  AND provider_snapshot IS NOT NULL
  AND (
      provider_key = 'easypay'
      OR provider_snapshot->>'provider_key' = 'easypay'
  )
  AND NULLIF(BTRIM(provider_snapshot->>'merchant_id'), '') IS NOT NULL;

UPDATE payment_orders
SET provider_gateway_identity = NULLIF(BTRIM(provider_snapshot->>'gateway_identity'), '')
WHERE NULLIF(BTRIM(provider_gateway_identity), '') IS NULL
  AND provider_snapshot IS NOT NULL
  AND NULLIF(BTRIM(provider_snapshot->>'gateway_identity'), '') IS NOT NULL;
