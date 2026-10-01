package attachment

import (
	"path"
	"strings"

	"github.com/google/uuid"
)

const MaxUploadBytes = 100 << 20

const maxExtLength = 8

type URLBuilder struct {
	publicURL string
	bucket    string
}

func NewURLBuilder(publicURL, bucket string) URLBuilder {
	return URLBuilder{publicURL: strings.TrimRight(publicURL, "/"), bucket: bucket}
}

func (b URLBuilder) URL(key string) string {
	return b.publicURL + "/" + b.bucket + "/" + key
}

func (b URLBuilder) Key(url string) (string, bool) {
	prefix := b.publicURL + "/" + b.bucket + "/"
	key, ok := strings.CutPrefix(url, prefix)
	if !ok || key == "" || strings.Contains(key, "..") {
		return "", false
	}
	return key, true
}

func newObjectBase(userID uuid.UUID) string {
	return userID.String() + "/" + uuid.NewString()
}

func NewObjectKey(userID uuid.UUID, filename string) string {
	key := newObjectBase(userID)
	if ext := sanitizeExt(filename); ext != "" {
		key += "." + ext
	}
	return key
}

func sanitizeExt(filename string) string {
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(filename), "."))
	if ext == "" || len(ext) > maxExtLength {
		return ""
	}
	for _, r := range ext {
		if (r < 'a' || r > 'z') && (r < '0' || r > '9') {
			return ""
		}
	}
	return ext
}
