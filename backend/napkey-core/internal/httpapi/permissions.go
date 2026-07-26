package httpapi

const (
	permissionUsersRead        = "users.read"
	permissionUsersWrite       = "users.write"
	permissionKeysWrite        = "keys.write"
	permissionBillingRead      = "billing.read"
	permissionBillingReconcile = "billing.reconcile"
	permissionPricesRead       = "prices.read"
	permissionPricesWrite      = "prices.write"
	permissionAuditRead        = "audit.read"
	permissionOperationsRead   = "operations.read"
)

var ownerPermissions = []string{
	permissionUsersRead, permissionUsersWrite, permissionKeysWrite,
	permissionBillingRead, permissionBillingReconcile,
	permissionPricesRead, permissionPricesWrite, permissionAuditRead,
	permissionOperationsRead,
}

func containsPermission(permissions []string, wanted string) bool {
	for _, permission := range permissions {
		if permission == wanted {
			return true
		}
	}
	return false
}
