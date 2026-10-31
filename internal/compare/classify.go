// Package compare decides how one request's two outcomes relate. Its central
// rule: agreement counts only with evidence. Two failures, or two responses
// whose bodies were not captured, are never a match (ADR 0003).
package compare

import (
	"fmt"

	"github.com/Hanart-prog/ghosttraffic/internal/metrics"
	"github.com/Hanart-prog/ghosttraffic/internal/report"
)

func sent(r metrics.Result) bool {
	switch r.ErrorCategory {
	case metrics.ErrorDropped, metrics.ErrorCancelled, metrics.ErrorDependency:
		return false
	}
	return true
}

// Classify compares one request across targets. Digests must be of the
// canonicalized, ignore-path-filtered body.
func Classify(baseline, candidate metrics.Result) report.Classification {
	switch {
	case !sent(baseline) || !sent(candidate):
		return report.ClassNotComparable
	case baseline.ErrorCategory != metrics.ErrorNone || candidate.ErrorCategory != metrics.ErrorNone:
		return report.ClassTransportFailure
	case baseline.Status != candidate.Status:
		return report.ClassStatusMismatch
	case baseline.Status >= 400:
		return report.ClassBothFailed
	case baseline.ResponseDigest == "" || candidate.ResponseDigest == "":
		return report.ClassBodyUnavailable
	case baseline.ResponseDigest != candidate.ResponseDigest:
		return report.ClassBodyMismatch
	}
	return report.ClassMatch
}

// Warnings flags results that could be mistaken for agreement.
func Warnings(stats []report.ComparisonStat) []string {
	warnings := []string{}
	for _, s := range stats {
		switch s.Classification {
		case report.ClassBothFailed:
			warnings = append(warnings, fmt.Sprintf("%s: %d requests failed the same way on both targets, which is not evidence of agreement; if they use recorded IDs, a workflow may fix them", s.Endpoint, s.Count))
		case report.ClassBodyUnavailable:
			warnings = append(warnings, fmt.Sprintf("%s: %d responses had no captured body; only their status codes were compared", s.Endpoint, s.Count))
		}
	}
	return warnings
}
