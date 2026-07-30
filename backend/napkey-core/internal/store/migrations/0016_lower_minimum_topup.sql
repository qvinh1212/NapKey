-- Keep the database boundary aligned with the 10,000 VND minimum enforced by
-- the API and shown in the checkout UI. The original constraint was 20,000 VND.
ALTER TABLE topup_orders
    DROP CONSTRAINT topup_expected_check,
    ADD CONSTRAINT topup_expected_check
        CHECK (expected_amount_micros >= 10000000000);

