package admincrud

import (
	"fmt"
	"strconv"
	"strings"

	"connectrpc.com/connect"
)

func Etag(version int64) string {
	return fmt.Sprintf(`W/"%d"`, version)
}

func ParseEtag(raw string) (int64, error) {
	value := strings.TrimPrefix(strings.TrimSpace(raw), "W/")
	value = strings.Trim(value, `"`)
	version, err := strconv.ParseInt(value, 10, 64)
	if err != nil || version < 1 {
		return 0, Invalid("etag", "etag", "etag is not valid")
	}
	return version, nil
}

func Conflict(current int64) error {
	return withInfo(connect.CodeAborted, "this record was changed by someone else; reload it and apply your changes again", ReasonEtagMismatch, map[string]string{"etag": Etag(current)})
}
