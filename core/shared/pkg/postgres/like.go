package postgres

import "strings"

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func Containing(s string) string {
	return "%" + likeEscaper.Replace(s) + "%"
}

func StartingWith(s string) string {
	return likeEscaper.Replace(s) + "%"
}
