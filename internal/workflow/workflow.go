// Package workflow loads explicit stateful workflows: which requests depend on
// which, which response values to extract, and where to substitute them. Nothing
// is inferred. Variables are per target and per session, because generated IDs
// differ between backends.
package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"slices"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Hanart-prog/ghosttraffic/internal/capture"
	"github.com/Hanart-prog/ghosttraffic/internal/trace"
)

// Failure policies. Skip is the default: a step whose prerequisite failed sends
// nothing, because it would act on state that does not exist.
const (
	OnFailureSkip     = "skip"
	OnFailureContinue = "continue"
)

// File is the on-disk workflow format.
type File struct {
	Workflows []Workflow `yaml:"workflows"`
}

// Workflow is the ordered work of one session.
type Workflow struct {
	SessionID           string `yaml:"session_id"`
	OnDependencyFailure string `yaml:"on_dependency_failure"`
	Steps               []Step `yaml:"steps"`
}

// Step ties one recorded request to its dependencies and variables.
type Step struct {
	ID         string            `yaml:"id"`
	RequestID  string            `yaml:"request_id"`
	DependsOn  []string          `yaml:"depends_on"`
	Extract    map[string]string `yaml:"extract"`
	Substitute Substitute        `yaml:"substitute"`
}

// Substitute rewrites a recorded request. Path maps a literal substring, which
// must occur exactly once, to a template. Body maps a JSON path to a template.
type Substitute struct {
	Path map[string]string `yaml:"path"`
	Body map[string]string `yaml:"body"`
}

// Load reads a workflow file. Unknown keys are errors.
func Load(path string) (File, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return File{}, fmt.Errorf("read workflows: %w", err)
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	var file File
	if err := dec.Decode(&file); err != nil {
		return File{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return file, nil
}

// Compiled is a validated step, keyed by request ID in a Plan.
type Compiled struct {
	Step
	Session  string
	After    []string // request IDs of direct dependencies
	Continue bool
}

// Plan is a File validated against one trace.
type Plan struct {
	Steps map[string]*Compiled
}

var (
	variable = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}`)
	exact    = regexp.MustCompile(`^\$\{([A-Za-z_][A-Za-z0-9_]*)\}$`)
)

// Compile validates every workflow against the trace, so a bad file fails
// before anything is sent.
func Compile(file File, records []trace.Record) (*Plan, error) {
	byID := make(map[string]trace.Record, len(records))
	for _, r := range records {
		byID[r.RequestID] = r
	}
	plan := &Plan{Steps: map[string]*Compiled{}}
	sessions := map[string]bool{}
	for _, w := range file.Workflows {
		if w.SessionID == "" {
			return nil, errors.New("workflow: session_id is required")
		}
		if sessions[w.SessionID] {
			return nil, fmt.Errorf("workflow: duplicate session %q", w.SessionID)
		}
		sessions[w.SessionID] = true
		if err := compileSession(w, byID, plan); err != nil {
			return nil, fmt.Errorf("workflow %s: %w", w.SessionID, err)
		}
	}
	return plan, nil
}

func compileSession(w Workflow, records map[string]trace.Record, plan *Plan) error {
	policy := w.OnDependencyFailure
	if policy == "" {
		policy = OnFailureSkip
	}
	if policy != OnFailureSkip && policy != OnFailureContinue {
		return fmt.Errorf("unknown on_dependency_failure %q", policy)
	}
	steps := make(map[string]Step, len(w.Steps))
	for _, step := range w.Steps {
		if step.ID == "" {
			return errors.New("step id is required")
		}
		if _, dup := steps[step.ID]; dup {
			return fmt.Errorf("duplicate step %q", step.ID)
		}
		steps[step.ID] = step
	}
	order, err := sortSteps(w.Steps, steps)
	if err != nil {
		return err
	}
	available := map[string]map[string]bool{} // step -> variables it may read
	for _, id := range order {
		step := steps[id]
		record, ok := records[step.RequestID]
		if !ok {
			return fmt.Errorf("step %q: request %q is not in the replayable trace", id, step.RequestID)
		}
		if _, dup := plan.Steps[step.RequestID]; dup {
			return fmt.Errorf("step %q: request %q is already used by another step", id, step.RequestID)
		}
		reachable := map[string]bool{}
		compiled := &Compiled{Step: step, Session: w.SessionID, Continue: policy == OnFailureContinue}
		for _, dep := range step.DependsOn {
			compiled.After = append(compiled.After, steps[dep].RequestID)
			for name := range available[dep] {
				reachable[name] = true
			}
			for name := range steps[dep].Extract {
				reachable[name] = true
			}
		}
		if err := checkStep(step, record, reachable); err != nil {
			return fmt.Errorf("step %q: %w", id, err)
		}
		available[id] = reachable
		plan.Steps[step.RequestID] = compiled
	}
	return nil
}

// sortSteps orders steps so dependencies come first, and rejects cycles.
func sortSteps(list []Step, steps map[string]Step) ([]string, error) {
	const (
		visiting = 1
		done     = 2
	)
	state := map[string]int{}
	var order []string
	var visit func(id string) error
	visit = func(id string) error {
		switch state[id] {
		case done:
			return nil
		case visiting:
			return fmt.Errorf("dependency cycle at step %q", id)
		}
		state[id] = visiting
		for _, dep := range steps[id].DependsOn {
			if _, ok := steps[dep]; !ok {
				return fmt.Errorf("step %q depends on unknown step %q", id, dep)
			}
			if err := visit(dep); err != nil {
				return err
			}
		}
		state[id] = done
		order = append(order, id)
		return nil
	}
	for _, step := range list {
		if err := visit(step.ID); err != nil {
			return nil, err
		}
	}
	return order, nil
}

func checkStep(step Step, record trace.Record, reachable map[string]bool) error {
	for name, path := range step.Extract {
		if !exact.MatchString("${" + name + "}") {
			return fmt.Errorf("invalid variable name %q", name)
		}
		segments, err := capture.ParsePath(path)
		if err != nil {
			return err
		}
		if capture.HasWildcard(segments) {
			return fmt.Errorf("extract %q: a wildcard does not name one value", path)
		}
	}
	checkTemplate := func(template string) error {
		for _, match := range variable.FindAllStringSubmatch(template, -1) {
			if !reachable[match[1]] {
				return fmt.Errorf("variable %q is not extracted by any dependency", match[1])
			}
		}
		return nil
	}
	for literal, template := range step.Substitute.Path {
		if literal == "" || strings.Count(record.Path, literal) != 1 {
			return fmt.Errorf("path substitution %q must occur exactly once in %q", literal, record.Path)
		}
		if err := checkTemplate(template); err != nil {
			return err
		}
	}
	for path, template := range step.Substitute.Body {
		segments, err := capture.ParsePath(path)
		if err != nil {
			return err
		}
		if capture.HasWildcard(segments) {
			return fmt.Errorf("body substitution %q: wildcards are not supported", path)
		}
		doc, err := decode(record.BodyJSON)
		if err != nil || doc == nil {
			return fmt.Errorf("body substitution %q: request has no JSON body", path)
		}
		if _, ok := capture.LookupPath(doc, segments); !ok {
			return fmt.Errorf("body substitution %q: not present in the recorded body", path)
		}
		if err := checkTemplate(template); err != nil {
			return err
		}
	}
	return nil
}

func decode(body []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var doc any
	return doc, dec.Decode(&doc)
}

// Variables holds extracted values for one session on one target.
type Variables map[string]any

// ReadVariables reads a step's variables from its response into vars.
func (c *Compiled) ReadVariables(body []byte, vars Variables) error {
	if len(c.Extract) == 0 {
		return nil
	}
	doc, err := decode(body)
	if err != nil {
		return fmt.Errorf("response is not JSON: %w", err)
	}
	names := make([]string, 0, len(c.Extract))
	for name := range c.Extract {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		segments, err := capture.ParsePath(c.Extract[name])
		if err != nil {
			return err
		}
		value, ok := capture.LookupPath(doc, segments)
		if !ok {
			return fmt.Errorf("%s not found in response", c.Extract[name])
		}
		switch value.(type) {
		case string, json.Number, bool:
			vars[name] = value
		default:
			return fmt.Errorf("%s is not a single value", c.Extract[name])
		}
	}
	return nil
}

func render(template string, vars Variables, escape func(string) string) (string, error) {
	var missing error
	out := variable.ReplaceAllStringFunc(template, func(match string) string {
		value, ok := vars[match[2:len(match)-1]]
		if !ok {
			missing = fmt.Errorf("variable %s has no value", match)
			return ""
		}
		return escape(fmt.Sprint(value))
	})
	return out, missing
}

// Apply returns the path and body to send. Values came from a target's response,
// so they are escaped: a value containing "/" or "?" cannot change the route.
func (c *Compiled) Apply(record trace.Record, vars Variables) (string, []byte, error) {
	path := record.Path
	literals := make([]string, 0, len(c.Substitute.Path))
	for literal := range c.Substitute.Path {
		literals = append(literals, literal)
	}
	slices.Sort(literals)
	for _, literal := range literals {
		rendered, err := render(c.Substitute.Path[literal], vars, url.PathEscape)
		if err != nil {
			return "", nil, err
		}
		path = strings.Replace(path, literal, rendered, 1)
	}
	body := []byte(record.BodyJSON)
	if len(c.Substitute.Body) == 0 {
		return path, body, nil
	}
	doc, err := decode(body)
	if err != nil {
		return "", nil, err
	}
	for jsonPath, template := range c.Substitute.Body {
		segments, err := capture.ParsePath(jsonPath)
		if err != nil {
			return "", nil, err
		}
		var value any
		if match := exact.FindStringSubmatch(template); match != nil {
			var ok bool
			if value, ok = vars[match[1]]; !ok {
				return "", nil, fmt.Errorf("variable %s has no value", template)
			}
		} else if value, err = render(template, vars, func(s string) string { return s }); err != nil {
			return "", nil, err
		}
		capture.ReplacePath(doc, segments, value)
	}
	body, err = json.Marshal(doc)
	return path, body, err
}
