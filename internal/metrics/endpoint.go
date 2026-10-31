package metrics

import (
	"fmt"
	"regexp"
	"strings"
)

// generatedSegment matches path segments that are runtime IDs rather than route
// names: integers, UUIDs, and long hex strings.
var generatedSegment = regexp.MustCompile(`^(\d+|[0-9a-fA-F]{8}-[0-9a-fA-F-]{27}|[0-9a-fA-F]{16,})$`)

// Router maps request paths to endpoint keys, so raw IDs never split one route
// into thousands of endpoints.
type Router struct {
	templates [][]string
}

// NewRouter compiles route templates such as /api/carts/{id}/items.
func NewRouter(templates []string) (*Router, error) {
	r := &Router{}
	for _, template := range templates {
		if !strings.HasPrefix(template, "/") {
			return nil, fmt.Errorf("route template %q must start with /", template)
		}
		r.templates = append(r.templates, strings.Split(template, "/"))
	}
	return r, nil
}

// Key returns "METHOD /route". The query is ignored. A path matching no
// template falls back to replacing generated-looking segments with {id}.
func (r *Router) Key(method, path string) string {
	path, _, _ = strings.Cut(path, "?")
	segments := strings.Split(path, "/")
	for _, template := range r.templates {
		if matches(template, segments) {
			return method + " " + strings.Join(template, "/")
		}
	}
	for i, segment := range segments {
		if generatedSegment.MatchString(segment) {
			segments[i] = "{id}"
		}
	}
	return method + " " + strings.Join(segments, "/")
}

func matches(template, segments []string) bool {
	if len(template) != len(segments) {
		return false
	}
	for i, part := range template {
		isParam := strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}")
		if !isParam && part != segments[i] {
			return false
		}
	}
	return true
}
