package admincrud

import "connectrpc.com/validate"

func Validator() *validate.Interceptor {
	return validate.NewInterceptor()
}
