package internalrpc

import (
	"crypto/subtle"
	"net/http"
)

const Header = "X-Axon-Internal-Key"

func Authorized(headers http.Header, key string) bool {
	return len(key) >= 32 && subtle.ConstantTimeCompare([]byte(headers.Get(Header)), []byte(key)) == 1
}
