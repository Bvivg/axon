package authz

import "strings"

type Permission struct {
	Namespace string
	Action    string
}

func (p Permission) String() string { return p.Namespace + ":" + p.Action }

var (
	UsersRead     = Permission{Namespace: "admin.users", Action: "read"}
	UsersMutate   = Permission{Namespace: "admin.users", Action: "mutate"}
	GamesRead     = Permission{Namespace: "admin.games", Action: "read"}
	GamesModerate = Permission{Namespace: "admin.games", Action: "moderate"}
	ChatRead      = Permission{Namespace: "admin.chat", Action: "read"}
	ChatModerate  = Permission{Namespace: "admin.chat", Action: "moderate"}
	AuditRead     = Permission{Namespace: "admin.audit", Action: "read"}
	RolesRead     = Permission{Namespace: "rbac", Action: "read"}
	RolesMutate   = Permission{Namespace: "rbac", Action: "mutate"}
)

type Set map[string]bool

func NewSet(granted []string) Set {
	set := make(Set, len(granted))
	for _, permission := range granted {
		if namespace, action, ok := strings.Cut(permission, ":"); ok && namespace != "" && action != "" {
			set[permission] = true
		}
	}
	return set
}

func (s Set) Has(p Permission) bool { return s[p.String()] }

func (s Set) Any(permissions ...Permission) bool {
	for _, p := range permissions {
		if s.Has(p) {
			return true
		}
	}
	return false
}
