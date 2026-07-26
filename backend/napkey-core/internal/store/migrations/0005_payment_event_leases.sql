-- A claimed webhook must become available again if a worker crashes mid-event.
ALTER TABLE payment_events ADD COLUMN processing_started_at timestamptz;
CREATE INDEX payment_events_processing_lease_idx
    ON payment_events(processing_started_at)
    WHERE status = 'processing';
