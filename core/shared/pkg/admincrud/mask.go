package admincrud

import (
	"slices"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/fieldmaskpb"

	"github.com/bvivg/axon/core/shared/pkg/authz"
)

type Fields map[string]authz.Permission

func (f Fields) Paths(mask *fieldmaskpb.FieldMask, granted authz.Set) ([]string, error) {
	if mask == nil || len(mask.GetPaths()) == 0 {
		return nil, Invalid("update_mask", "required", "choose at least one field to change")
	}
	paths := make([]string, 0, len(mask.GetPaths()))
	for _, path := range mask.GetPaths() {
		permission, known := f[path]
		if !known {
			return nil, Invalid("update_mask", "known_path", "field "+path+" cannot be changed")
		}
		if !granted.Has(permission) {
			return nil, withInfo(connect.CodePermissionDenied, "you do not have permission to change "+path, ReasonFieldPermissionDenied, map[string]string{"field": path})
		}
		if !slices.Contains(paths, path) {
			paths = append(paths, path)
		}
	}
	slices.Sort(paths)
	return paths, nil
}
