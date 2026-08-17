-- Return the retail credit rate to the 75 VND the site advertises.
--
-- 0017 moved it to 400 and, following 0010 and 0014, rescaled every wallet balance
-- by 400/75 so a customer's displayed credit count survived the change. This
-- migration deliberately does NOT do the reverse. Dividing balance_micros by 400/75
-- would take 5.33x of real money off every wallet: the micros column is VND, not
-- credits, and it is the money customers actually paid. Only the divisor used to
-- display that money as credits changes here, so a balance now reads 5.33x more
-- credits -- which is the point, because 75 is the rate the price page and the
-- top-up form quote and 400 was never shown to a buyer before they paid.
--
-- Unpaid orders are repriced because the credit figure on them is a quote, not a
-- settled amount. Paid and underpaid orders keep the rate they were bought at, so a
-- past receipt still reconciles against the ledger row it produced.
LOCK TABLE topup_orders IN ACCESS EXCLUSIVE MODE;

ALTER TABLE topup_orders ALTER COLUMN retail_vnd_per_credit SET DEFAULT 75;
UPDATE topup_orders SET retail_vnd_per_credit = 75 WHERE status <> 'paid' AND status <> 'underpaid' AND received_amount_micros = 0;
