package storage

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestCloudinaryFallbackValidatesFullResponse(t *testing.T) {
	fallback := cloudinaryFallback{dir: t.TempDir(), limit: 4}
	t.Cleanup(fallback.evict)
	info := FileInfo{Object: Object{Key: "asset", Size: 4}, Revision: "1"}
	buffer := make([]byte, 2)
	for _, sample := range []struct {
		contents string
		want     error
	}{
		{"abc", io.EOF},
		{"abcde", ErrFileChanged},
		{"abcd", nil},
	} {
		response := &http.Response{Body: io.NopCloser(strings.NewReader(sample.contents)), ContentLength: -1}
		_, err := fallback.readFull(response, info, 1, buffer)
		if !errors.Is(err, sample.want) {
			t.Fatalf("body %q: got %v, want %v", sample.contents, err, sample.want)
		}
		if (fallback.file != nil) != (sample.want == nil) {
			t.Fatalf("body %q: unexpected cache state", sample.contents)
		}
	}
	if string(buffer) != "bc" {
		t.Fatalf("read %q, want bc", buffer)
	}
	info.Size++
	response := &http.Response{Body: io.NopCloser(strings.NewReader("abcde")), ContentLength: -1}
	if _, err := fallback.readFull(response, info, 1, buffer); !errors.Is(err, ErrRangeUnsupported) {
		t.Fatalf("over-budget response: %v", err)
	}
}
