package report

import (
	"bytes"
	_ "embed"
	"fmt"
	"html/template"
	"strings"
)

//go:embed report.html.tmpl
var reportTemplate string

var page = template.Must(template.New("report").Funcs(template.FuncMap{
	"ms":      formatMS,
	"plain":   func(s any) string { return strings.ReplaceAll(fmt.Sprint(s), "_", " ") },
	"chart":   trialChart,
	"judged":  func(v Verdict) bool { return v == VerdictObserved || v == VerdictConfirmed },
	"shorten": func(s string) string { return s[:min(len(s), 12)] },
}).Parse(reportTemplate))

// RenderHTML renders a standalone page with no external assets. html/template
// escapes every value, so recorded paths cannot inject markup.
func RenderHTML(r Report) ([]byte, error) {
	var buf bytes.Buffer
	if err := page.Execute(&buf, r); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func formatMS(v *float64) string {
	if v == nil {
		return "n/a"
	}
	return fmt.Sprintf("%.1f ms", *v)
}

const (
	chartWidth  = 520.0
	rowHeight   = 26.0
	labelWidth  = 56.0
	rightMargin = 12.0
	axisHeight  = 22.0
)

// scaleMax is the axis limit shared by every endpoint's chart. A separate scale
// per chart would draw 1 ms of noise as large as a real 10 ms regression.
func scaleMax(r Report) float64 {
	limit := 0.0
	for _, e := range r.Endpoints {
		for _, t := range e.Trials {
			for _, v := range []*float64{t.BaselineP95MS, t.CandidateP95MS} {
				if v != nil {
					limit = max(limit, *v)
				}
			}
		}
	}
	return limit * 1.05
}

// trialChart draws one row per trial with a dot per target, so a reader sees the
// spread across trials and not only a pooled number. Scale starts at zero, and
// the two series sit on separate lines so equal values cannot hide each other.
func trialChart(r Report, e Endpoint) template.HTML {
	maxMS := scaleMax(r)
	if maxMS == 0 {
		return ""
	}
	plot := chartWidth - labelWidth - rightMargin
	x := func(v float64) float64 { return labelWidth + v/maxMS*plot }
	height := float64(len(e.Trials))*rowHeight + axisHeight

	var b strings.Builder
	fmt.Fprintf(&b, `<svg viewBox="0 0 %.0f %.0f" role="img" aria-label="p95 response time per trial for %s">`,
		chartWidth, height, template.HTMLEscapeString(e.Key))
	for i, t := range e.Trials {
		y := float64(i)*rowHeight + rowHeight/2
		fmt.Fprintf(&b, `<text class="axis" x="%.0f" y="%.1f" text-anchor="end" dominant-baseline="middle">trial %d</text>`, labelWidth-8, y, t.Trial)
		fmt.Fprintf(&b, `<line class="grid" x1="%.0f" x2="%.1f" y1="%.1f" y2="%.1f"/>`, labelWidth, chartWidth-rightMargin, y, y)
		dot := func(class, name string, v *float64, offset float64) {
			if v != nil {
				fmt.Fprintf(&b, `<circle class="dot %s" cx="%.1f" cy="%.1f" r="5"><title>Trial %d %s: %.1f ms</title></circle>`, class, x(*v), y+offset, t.Trial, name, *v)
			}
		}
		dot("baseline", "baseline", t.BaselineP95MS, -4)
		dot("candidate", "candidate", t.CandidateP95MS, 4)
	}
	axis := float64(len(e.Trials))*rowHeight + 14
	fmt.Fprintf(&b, `<text class="axis" x="%.0f" y="%.1f">0</text>`, labelWidth, axis)
	fmt.Fprintf(&b, `<text class="axis" x="%.0f" y="%.1f" text-anchor="end">%.0f ms</text>`, chartWidth-rightMargin, axis, maxMS)
	b.WriteString(`</svg>`)
	return template.HTML(b.String())
}
