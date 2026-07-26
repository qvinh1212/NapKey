package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

func (s *Store) AdminPermissions(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT DISTINCT p.permission
		FROM user_admin_roles ur
		JOIN admin_role_permissions p ON p.role_id = ur.role_id
		WHERE ur.user_id = $1
		ORDER BY p.permission`, userID)
	if err != nil {
		return nil, fmt.Errorf("store: listing admin permissions: %w", err)
	}
	defer rows.Close()

	permissions := make([]string, 0)
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return nil, fmt.Errorf("store: scanning admin permission: %w", err)
		}
		permissions = append(permissions, permission)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: iterating admin permissions: %w", err)
	}
	return permissions, nil
}

func (s *Store) SetAdminRoles(ctx context.Context, userID, grantedBy string, roleNames []string) error {
	return s.withTx(ctx, nil, func(tx *sql.Tx) error {
		var exists bool
		if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists); err != nil {
			return fmt.Errorf("store: checking admin role user: %w", err)
		}
		if !exists {
			return ErrNotFound
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM user_admin_roles WHERE user_id = $1`, userID); err != nil {
			return fmt.Errorf("store: clearing admin roles: %w", err)
		}
		for _, name := range roleNames {
			result, err := tx.ExecContext(ctx, `
				INSERT INTO user_admin_roles (user_id, role_id, granted_by)
				SELECT $1, id, $2 FROM admin_roles WHERE name = $3`, userID, grantedBy, name)
			if err != nil {
				return fmt.Errorf("store: assigning admin role: %w", err)
			}
			rows, err := result.RowsAffected()
			if err != nil || rows != 1 {
				return fmt.Errorf("store: unknown admin role %q", name)
			}
		}
		return nil
	})
}

func (s *Store) AdminRoles(ctx context.Context, userID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT r.name FROM user_admin_roles ur JOIN admin_roles r ON r.id = ur.role_id WHERE ur.user_id = $1 ORDER BY r.name`, userID)
	if errors.Is(err, sql.ErrNoRows) {
		return []string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("store: listing admin roles: %w", err)
	}
	defer rows.Close()
	roles := make([]string, 0)
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, fmt.Errorf("store: scanning admin role: %w", err)
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}
