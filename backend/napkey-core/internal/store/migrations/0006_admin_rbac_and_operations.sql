CREATE TABLE admin_roles (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    name        text NOT NULL UNIQUE,
    description text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE admin_role_permissions (
    role_id    uuid NOT NULL REFERENCES admin_roles(id) ON DELETE CASCADE,
    permission text NOT NULL,
    PRIMARY KEY (role_id, permission)
);

CREATE TABLE user_admin_roles (
    user_id    uuid NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    role_id    uuid NOT NULL REFERENCES admin_roles(id) ON DELETE CASCADE,
    granted_by uuid REFERENCES users(id) ON DELETE SET NULL,
    granted_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, role_id)
);

INSERT INTO admin_roles (name, description) VALUES
    ('owner', 'Full operational access'),
    ('operator', 'Operate users, keys and platform health'),
    ('support', 'Read users and audit activity'),
    ('finance', 'Read billing and run reconciliation'),
    ('viewer', 'Read-only operational visibility');

WITH permissions(role_name, permission) AS (VALUES
    ('owner', 'users.read'), ('owner', 'users.write'), ('owner', 'keys.write'),
    ('owner', 'billing.read'), ('owner', 'billing.reconcile'),
    ('owner', 'prices.read'), ('owner', 'prices.write'), ('owner', 'audit.read'),
    ('owner', 'operations.read'),
    ('operator', 'users.read'), ('operator', 'users.write'), ('operator', 'keys.write'),
    ('operator', 'billing.read'), ('operator', 'audit.read'), ('operator', 'operations.read'),
    ('support', 'users.read'), ('support', 'audit.read'), ('support', 'operations.read'),
    ('finance', 'billing.read'), ('finance', 'billing.reconcile'), ('finance', 'prices.read'),
    ('finance', 'audit.read'), ('finance', 'operations.read'),
    ('viewer', 'users.read'), ('viewer', 'billing.read'), ('viewer', 'prices.read'),
    ('viewer', 'audit.read'), ('viewer', 'operations.read')
)
INSERT INTO admin_role_permissions (role_id, permission)
SELECT r.id, p.permission FROM permissions p JOIN admin_roles r ON r.name = p.role_name;

CREATE TABLE operations_alerts (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    alert_type  text NOT NULL,
    severity    text NOT NULL CHECK (severity IN ('info', 'warning', 'critical')),
    fingerprint text NOT NULL,
    status      text NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'resolved')),
    title       text NOT NULL,
    metadata    jsonb NOT NULL DEFAULT '{}'::jsonb,
    opened_at   timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz
);

CREATE UNIQUE INDEX operations_alerts_open_fingerprint_idx
    ON operations_alerts (fingerprint) WHERE status = 'open';
CREATE INDEX operations_alerts_status_opened_idx ON operations_alerts (status, opened_at DESC);
