//go:build unit

package service

import (
	"github.com/stretchr/testify/require"
	"strings"
	"testing"
)

func TestPelicanEvaluationSafePreviewAndIsolation(t *testing.T) {
	svg := `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 500 300"><circle cx="100" cy="220" r="40"/><circle cx="350" cy="220" r="40"/>` + strings.Repeat(`<path d="M100 100 L200 200"/>`, 6) + `</svg>`
	r := &ScheduledTestResult{TestKind: ScheduledTestPelicanV1, Status: "success", ResponseText: "```svg\n" + svg + "\n```"}
	EvaluateReasoningProbe(r)
	require.Nil(t, r.Quality, "SVG must never change candy verdict / auto-exclusion")
	require.Equal(t, "reference_only", r.Pelican.Verdict)
	require.Equal(t, 100, r.Pelican.Score)
	require.Equal(t, svg, r.Pelican.SVG)
	for _, bad := range []string{
		`<svg><script>alert(1)</script></svg>`, `<svg onload="alert(1)"></svg>`,
		`<svg><image href="https://example.com/a.png"/></svg>`,
		`<svg><foreignObject/></svg>`, `<svg><path fill="url(https://example.com/a)"/></svg>`,
		`<svg><style>@import 'https://example.com';</style></svg>`,
		`<svg><g></svg>`, `<svg/>`, `<svg><text>&xxe;</text></svg>`,
		`<svg></svg><svg></svg>`, strings.Repeat("x", 256*1024+1),
	} {
		r := &ScheduledTestResult{TestKind: ScheduledTestPelicanV1, Status: "success", ResponseText: bad}
		EvaluatePelicanProbe(r)
		require.Empty(t, r.Pelican.SVG, bad)
		require.Equal(t, "invalid_svg", r.Pelican.Verdict)
	}
	r = &ScheduledTestResult{TestKind: ScheduledTestPelicanV1, Status: "failed", ResponseText: svg}
	EvaluatePelicanProbe(r)
	require.Equal(t, "inconclusive", r.Pelican.Verdict)
	require.Empty(t, r.Pelican.SVG)
	p := &ScheduledTestPlan{TestKind: ScheduledTestPelicanV1, AutoRecover: true}
	require.NoError(t, validateScheduledTestKind(p))
	require.False(t, p.AutoRecover)
	require.Equal(t, CandyReasoningModel, p.ModelID)
}
