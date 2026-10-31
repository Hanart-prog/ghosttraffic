package integration

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ADR 0005: outbound HTTP must go through internal/egress. This keeps a later
// engine from silently building a client with no dial check or redirect guard.
func TestOutboundHTTPOnlyThroughEgress(t *testing.T) {
	forbidden := []string{"http.Client{", "http.Transport{", "http.DefaultClient", "http.DefaultTransport", "http.Get(", "http.Post(", "http.Head(", "http.PostForm(", "net.Dial("}
	for _, root := range []string{"../../cmd", "../../internal", "../../pkg"} {
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
				return nil
			}
			if filepath.ToSlash(filepath.Dir(path)) == "../../internal/egress" {
				return nil
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			for _, pattern := range forbidden {
				if strings.Contains(string(data), pattern) {
					t.Errorf("%s uses %s; use internal/egress", path, pattern)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
}
