package domain

import "github.com/google/uuid"

type UserSearchCursor struct {
	Rank     int       `json:"r"`
	Name     string    `json:"n"`
	Username string    `json:"u"`
	ID       uuid.UUID `json:"i"`
}

type UserSearchResult struct {
	User
	Cursor UserSearchCursor
}
