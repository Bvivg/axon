package repository

import (
	"fmt"
	"strings"

	"github.com/google/uuid"

	"github.com/bvivg/axon/core/shared/pkg/adminquery"
)

func equalsOneOf(column string, values ...string) map[string]string {
	out := make(map[string]string, len(values))
	for _, v := range values {
		out[v] = column + " = '" + v + "'"
	}
	return out
}

func matchesID(field, predicate string) func(adminquery.Op, string) (adminquery.Condition, error) {
	return func(op adminquery.Op, value string) (adminquery.Condition, error) {
		id, err := uuid.Parse(value)
		if err != nil {
			return adminquery.Condition{}, fmt.Errorf("needs a UUID for %q", field)
		}
		args := make([]any, strings.Count(predicate, "?"))
		for i := range args {
			args[i] = id
		}
		switch op {
		case adminquery.OpEq, adminquery.OpHas:
			return adminquery.Condition{SQL: predicate, Args: args}, nil
		case adminquery.OpNe:
			return adminquery.Condition{SQL: "NOT " + predicate, Args: args}, nil
		}
		return adminquery.Condition{}, fmt.Errorf("does not support %q on %q", op, field)
	}
}
