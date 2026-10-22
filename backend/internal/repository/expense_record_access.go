package repository

// CanEditExpenseRecord preserves category-wide Edit and adds role-scoped edits
// for Add users. Creator roles come from the recorded user ID, never a name or
// an imported label. Missing creators and unassigned roles fail closed.
func CanEditExpenseRecord(user AuthUser, categoryID, creatorRoleID string) bool {
	for _, access := range user.ExpenseCategoryAccess {
		if access.CategoryID == categoryID {
			return access.CanEdit || (access.CanView && access.CanCreate && user.RoleID != "" && user.RoleID == creatorRoleID)
		}
	}
	return false
}
