package domain

import "github.com/google/uuid"

var (
	RoleUser  = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	RoleAdmin = uuid.MustParse("00000000-0000-0000-0000-000000000002")
)

type Permission struct {
	ID        uuid.UUID
	Namespace string
	Action    string
	ParentID  *uuid.UUID
}

type Role struct {
	ID          uuid.UUID
	Name        string
	Permissions []Permission
}
