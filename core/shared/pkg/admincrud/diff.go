package admincrud

import (
	"fmt"
	"slices"
	"strings"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/bvivg/axon/core/shared/pkg/authz"
)

const Redacted = "[REDACTED]"

func Diff(before, after proto.Message, paths []string, sensitive ...string) []authz.Change {
	var changes []authz.Change
	for _, path := range paths {
		was, now := field(before, path), field(after, path)
		if was == now {
			continue
		}
		if slices.Contains(sensitive, path) {
			was, now = Redacted, Redacted
		}
		changes = append(changes, authz.Change{Field: path, Before: was, After: now})
	}
	return changes
}

func Snapshot(message proto.Message, sensitive ...string) []authz.Change {
	if message == nil {
		return nil
	}
	var changes []authz.Change
	reflected := message.ProtoReflect()
	fields := reflected.Descriptor().Fields()
	for i := range fields.Len() {
		if !reflected.Has(fields.Get(i)) {
			continue
		}
		name := string(fields.Get(i).Name())
		value := field(message, name)
		if slices.Contains(sensitive, name) {
			value = Redacted
		}
		changes = append(changes, authz.Change{Field: name, Before: value})
	}
	return changes
}

func field(message proto.Message, path string) string {
	if message == nil {
		return ""
	}
	reflected := message.ProtoReflect()
	descriptor := reflected.Descriptor().Fields().ByName(protoreflect.Name(path))
	if descriptor == nil || (!descriptor.IsList() && !descriptor.IsMap() && descriptor.HasPresence() && !reflected.Has(descriptor)) {
		return ""
	}
	return format(descriptor, reflected.Get(descriptor))
}

func format(descriptor protoreflect.FieldDescriptor, value protoreflect.Value) string {
	switch {
	case descriptor.IsList():
		list := value.List()
		items := make([]string, list.Len())
		for i := range list.Len() {
			items[i] = scalar(descriptor, list.Get(i))
		}
		slices.Sort(items)
		return strings.Join(items, ", ")
	case descriptor.IsMap():
		var items []string
		value.Map().Range(func(key protoreflect.MapKey, item protoreflect.Value) bool {
			items = append(items, key.String()+"="+scalar(descriptor.MapValue(), item))
			return true
		})
		slices.Sort(items)
		return strings.Join(items, ", ")
	default:
		return scalar(descriptor, value)
	}
}

func scalar(descriptor protoreflect.FieldDescriptor, value protoreflect.Value) string {
	switch descriptor.Kind() {
	case protoreflect.MessageKind, protoreflect.GroupKind:
		raw, err := protojson.MarshalOptions{UseProtoNames: true}.Marshal(value.Message().Interface())
		if err != nil {
			return ""
		}
		return string(raw)
	case protoreflect.EnumKind:
		if enum := descriptor.Enum().Values().ByNumber(value.Enum()); enum != nil {
			return string(enum.Name())
		}
		return fmt.Sprint(value.Enum())
	case protoreflect.BytesKind:
		return fmt.Sprintf("%d bytes", len(value.Bytes()))
	default:
		return value.String()
	}
}
