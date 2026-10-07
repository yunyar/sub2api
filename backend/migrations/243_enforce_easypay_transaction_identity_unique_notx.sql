CREATE UNIQUE INDEX CONCURRENTLY IF NOT EXISTS paymentorder_easypay_trade_identity_unique
    ON payment_orders (provider_key, provider_gateway_identity, provider_merchant_id, payment_trade_no)
    WHERE provider_key = 'easypay'
      AND provider_gateway_identity IS NOT NULL
      AND TRIM(provider_gateway_identity) <> ''
      AND provider_merchant_id IS NOT NULL
      AND TRIM(provider_merchant_id) <> ''
      AND NULLIF(BTRIM(payment_trade_no), '') IS NOT NULL;
