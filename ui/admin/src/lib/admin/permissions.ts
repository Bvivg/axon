export enum Permission {
  UsersRead = "admin.users:read",
  UsersMutate = "admin.users:mutate",
  GamesRead = "admin.games:read",
  GamesModerate = "admin.games:moderate",
  ChatRead = "admin.chat:read",
  ChatModerate = "admin.chat:moderate",
  AuditRead = "admin.audit:read",
  RolesRead = "rbac:read",
  RolesMutate = "rbac:mutate",
}

export enum StaffRole {
  Admin = "admin",
  Moderator = "moderator",
}
