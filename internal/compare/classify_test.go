package compare

import (
	"strings"
	"testing"

	"github.com/Hanart-prog/ghosttraffic/internal/metrics"
	"github.com/Hanart-prog/ghosttraffic/internal/report"
)

func ok(status int, digest string) metrics.Result {
	return metrics.Result{Status: status, ErrorCategory: metrics.ErrorNone, ResponseDigest: digest}
}

func failed(category metrics.ErrorCategory) metrics.Result {
	return metrics.Result{ErrorCategory: category}
}

func TestClassify(t *testing.T) {
	tests := []struct {
		name string
		a, b metrics.Result
		want report.Classification
	}{
		{"same body", ok(200, "x"), ok(200, "x"), report.ClassMatch},
		{"different body", ok(200, "x"), ok(200, "y"), report.ClassBodyMismatch},
		{"different status", ok(200, "x"), ok(500, "x"), report.ClassStatusMismatch},
		{"timeout", ok(200, "x"), failed(metrics.ErrorTimeout), report.ClassTransportFailure},
		{"malformed", failed(metrics.ErrorMalformed), ok(200, "x"), report.ClassTransportFailure},
		{"reset", failed(metrics.ErrorReset), ok(200, "x"), report.ClassTransportFailure},
		{"blocked", ok(200, "x"), failed(metrics.ErrorBlocked), report.ClassTransportFailure},
		{"dropped", ok(200, "x"), failed(metrics.ErrorDropped), report.ClassNotComparable},
		{"cancelled", failed(metrics.ErrorCancelled), failed(metrics.ErrorTimeout), report.ClassNotComparable},

		{"prerequisite failed", ok(200, "x"), failed(metrics.ErrorDependency), report.ClassNotComparable},
		{"extraction failed", failed(metrics.ErrorExtraction), ok(200, "x"), report.ClassTransportFailure},

		{"both 404 with equal bodies", ok(404, "x"), ok(404, "x"), report.ClassBothFailed},
		{"both 500", ok(500, ""), ok(500, ""), report.ClassBothFailed},
		{"no bodies", ok(200, ""), ok(200, ""), report.ClassBodyUnavailable},
		{"one body missing", ok(200, "x"), ok(200, ""), report.ClassBodyUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.a, tt.b); got != tt.want {
				t.Fatalf("got %s, want %s", got, tt.want)
			}
		})
	}
}

func TestWarnings(t *testing.T) {
	warnings := Warnings([]report.ComparisonStat{
		{Endpoint: "GET /a", Classification: report.ClassBothFailed, Count: 3},
		{Endpoint: "GET /b", Classification: report.ClassBodyUnavailable, Count: 2},
		{Endpoint: "GET /c", Classification: report.ClassMatch, Count: 9},
	})
	if len(warnings) != 2 || !strings.Contains(warnings[0], "not evidence of agreement") {
		t.Fatalf("warnings = %v", warnings)
	}
}
