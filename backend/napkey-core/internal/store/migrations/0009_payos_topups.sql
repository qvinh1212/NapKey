-- PayOS checkout metadata. Historical Casso rows keep nullable provider fields.
ALTER TABLE topup_orders
    ALTER COLUMN bank_account_number DROP NOT NULL,
    ADD COLUMN provider_order_code bigint,
    ADD COLUMN provider_payment_link_id text,
    ADD COLUMN checkout_url text,
    ADD COLUMN qr_code text;

CREATE UNIQUE INDEX topup_orders_provider_order_code_idx
    ON topup_orders(provider, provider_order_code)
    WHERE provider_order_code IS NOT NULL;

