package admincrud

import (
	"errors"

	validatepb "buf.build/gen/go/bufbuild/protovalidate/protocolbuffers/go/buf/validate"
	"connectrpc.com/connect"
	"github.com/jackc/pgx/v5/pgconn"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/protobuf/proto"

	"github.com/bvivg/axon/core/shared/pkg/adminquery"
)

const Domain = "axon"

const (
	ReasonEtagMismatch          = "ETAG_MISMATCH"
	ReasonFieldPermissionDenied = "FIELD_PERMISSION_DENIED"
	ReasonValidation            = "VALIDATION_FAILED"
)

func FieldError(code connect.Code, field, rule, message string) error {
	err := connect.NewError(code, errors.New(message))
	violations := &validatepb.Violations{Violations: []*validatepb.Violation{{
		Field:   &validatepb.FieldPath{Elements: []*validatepb.FieldPathElement{{FieldName: proto.String(field)}}},
		RuleId:  proto.String(rule),
		Message: proto.String(message),
	}}}
	if detail, derr := connect.NewErrorDetail(violations); derr == nil {
		err.AddDetail(detail)
	}
	if detail, derr := connect.NewErrorDetail(&errdetails.ErrorInfo{Reason: ReasonValidation, Domain: Domain, Metadata: map[string]string{"field": field}}); derr == nil {
		err.AddDetail(detail)
	}
	return err
}

func Invalid(field, rule, message string) error {
	return FieldError(connect.CodeInvalidArgument, field, rule, message)
}

func Taken(field, message string) error {
	return FieldError(connect.CodeAlreadyExists, field, "unique", message)
}

func withInfo(code connect.Code, message, reason string, metadata map[string]string) error {
	err := connect.NewError(code, errors.New(message))
	if detail, derr := connect.NewErrorDetail(&errdetails.ErrorInfo{Reason: reason, Domain: Domain, Metadata: metadata}); derr == nil {
		err.AddDetail(detail)
	}
	return err
}

func UniqueViolation(err error, constraint string) bool {
	var pg *pgconn.PgError
	return errors.As(err, &pg) && pg.Code == "23505" && (constraint == "" || pg.ConstraintName == constraint)
}

func QueryError(err error) error {
	var qe *adminquery.Error
	if errors.As(err, &qe) {
		return Invalid(qe.Field, "query", qe.Error())
	}
	return err
}
