package repository

import "slices"

// Missing keys inherit the role. Explicit denies also disable dependent grants.
type PermissionOverrides map[string]string
type ExpenseCategoryOverrides map[string]PermissionOverrides

func overrideAllowed(inherited bool, value string) bool {
	switch value {
	case "allow":
		return true
	case "deny":
		return false
	default:
		return inherited
	}
}

func EffectivePermissions(role []string, overrides PermissionOverrides) []string {
	keys := []string{}
	for _, permission := range Permissions {
		if overrideAllowed(slices.Contains(role, permission.Key), overrides[permission.Key]) {
			keys = append(keys, permission.Key)
		}
	}
	// Iterate to a fixed point: prerequisites can themselves have prerequisites.
	for {
		next := []string{}
		for _, permission := range Permissions {
			if slices.Contains(keys, permission.Key) && !slices.ContainsFunc(permission.Requires, func(key string) bool { return !slices.Contains(keys, key) }) {
				next = append(next, permission.Key)
			}
		}
		if len(next) == len(keys) {
			return next
		}
		keys = next
	}
}

func effectiveCategoryAccess(access ExpenseCategoryAccess, overrides PermissionOverrides) ExpenseCategoryAccess {
	access.CanView = overrideAllowed(access.CanView, overrides["canView"])
	access.CanCreate = access.CanView && overrideAllowed(access.CanCreate, overrides["canCreate"])
	access.CanEdit = access.CanView && overrideAllowed(access.CanEdit, overrides["canEdit"])
	access.CanDelete = access.CanView && overrideAllowed(access.CanDelete, overrides["canDelete"])
	return access
}

func validUserOverrides(permissions PermissionOverrides, categories ExpenseCategoryOverrides) bool {
	for key, value := range permissions {
		if !slices.Contains(PermissionKeys(), key) || (value != "allow" && value != "deny") {
			return false
		}
	}
	for _, actions := range categories {
		if len(actions) == 0 {
			return false
		}
		for key, value := range actions {
			if !slices.Contains([]string{"canView", "canCreate", "canEdit", "canDelete"}, key) || (value != "allow" && value != "deny") {
				return false
			}
		}
	}
	return true
}
