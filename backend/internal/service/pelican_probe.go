package service

import (
	"context"
	"encoding/xml"
	"io"
	"math"
	"strconv"
	"strings"
	"time"
)

const ScheduledTestPelicanV1 = "pelican_svg_v1"
const PelicanPrompt = `Generate an SVG of a pelican riding a bicycle. Return only a complete, self-contained SVG with a viewBox. Use vector shapes only, with no scripts, external resources, embedded images, CSS style elements, or animation. Do not use any tools.`

// Structural checks are diagnostic evidence, not a model intelligence verdict.
// Reference: simonw/pelican-bicycle and the layered validation approach in
// huggingface/OpenEnv pelican_svg. No third-party implementation is copied.
type PelicanEvaluation struct {
	Version string         `json:"version"`
	Verdict string         `json:"verdict"`
	Score   int            `json:"score"`
	Checks  []PelicanCheck `json:"checks"`
	SVG     string         `json:"svg,omitempty"`
}
type PelicanCheck struct {
	Name   string `json:"name"`
	Passed bool   `json:"passed"`
}

func EvaluatePelicanProbe(r *ScheduledTestResult) {
	if r == nil || r.TestKind != ScheduledTestPelicanV1 {
		return
	}
	q := &PelicanEvaluation{Version: ScheduledTestPelicanV1, Verdict: "inconclusive", Checks: []PelicanCheck{}}
	r.Pelican = q
	if r.Status != "success" || strings.TrimSpace(r.ResponseText) == "" {
		return
	}
	q.Verdict = "invalid_svg"
	raw := r.ResponseText
	if len(raw) > 256*1024 {
		return
	}
	start, end := strings.Index(raw, "<svg"), strings.LastIndex(raw, "</svg>")
	if start < 0 || end < start {
		return
	}
	raw = raw[start : end+6]
	allowed := map[string]bool{}
	for _, tag := range strings.Fields("svg g path circle ellipse rect line polyline polygon defs linearGradient radialGradient stop title desc text tspan clipPath") {
		allowed[tag] = true
	}
	shapes, depth, roots, nodes := 0, 0, 0, 0
	canvas := false
	type wheel struct{ x, y, r float64 }
	var wheels []wheel
	dec := xml.NewDecoder(strings.NewReader(raw))
	for {
		token, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return
		}
		switch e := token.(type) {
		case xml.Directive, xml.ProcInst:
			return
		case xml.StartElement:
			nodes++
			if nodes > 5000 || depth > 64 || !allowed[e.Name.Local] {
				return
			}
			if e.Name.Space != "" && e.Name.Space != "http://www.w3.org/2000/svg" {
				return
			}
			if depth == 0 {
				roots++
				if e.Name.Local != "svg" || roots != 1 {
					return
				}
			}
			depth++
			attrs := map[string]string{}
			for _, a := range e.Attr {
				name, v := strings.ToLower(a.Name.Local), strings.ToLower(strings.TrimSpace(a.Value))
				if strings.HasPrefix(name, "on") || name == "href" || name == "src" || name == "base" {
					return
				}
				// Only local fragment paint/clip references are permitted.
				if strings.Contains(v, "url(") && (!strings.HasPrefix(v, "url(#") || !strings.HasSuffix(v, ")") || strings.Count(v, "url(") != 1) {
					return
				}
				if strings.ContainsAny(v, "\\@") || strings.Contains(v, "expression(") {
					return
				}
				attrs[a.Name.Local] = a.Value
			}
			if e.Name.Local == "svg" && depth == 1 {
				f := strings.Fields(strings.ReplaceAll(attrs["viewBox"], ",", " "))
				if len(f) == 4 {
					w, we := strconv.ParseFloat(f[2], 64)
					h, he := strconv.ParseFloat(f[3], 64)
					canvas = we == nil && he == nil && w > 0 && h > 0 && !math.IsInf(w, 0) && !math.IsInf(h, 0)
				}
			}
			if strings.Contains(" path circle ellipse rect line polyline polygon ", " "+e.Name.Local+" ") {
				shapes++
			}
			if e.Name.Local == "circle" {
				x, ex := strconv.ParseFloat(attrs["cx"], 64)
				y, ey := strconv.ParseFloat(attrs["cy"], 64)
				radius, er := strconv.ParseFloat(attrs["r"], 64)
				if ex == nil && ey == nil && er == nil && radius > 0 {
					wheels = append(wheels, wheel{x, y, radius})
				}
			}
		case xml.EndElement:
			depth--
		}
	}
	if depth != 0 || roots != 1 {
		return
	}
	pair := false
	for i, a := range wheels {
		for _, b := range wheels[i+1:] {
			if math.Min(a.r, b.r)/math.Max(a.r, b.r) >= 0.65 && math.Abs(a.y-b.y) <= math.Max(a.r, b.r)*0.5 && math.Abs(a.x-b.x) > 2*math.Max(a.r, b.r) {
				pair = true
			}
		}
	}
	q.SVG = raw
	q.Verdict = "reference_only"
	q.Checks = []PelicanCheck{{"safe_svg", true}, {"viewbox", canvas}, {"vector_shapes", shapes >= 8}, {"wheel_pair", pair}}
	for _, c := range q.Checks {
		if c.Passed {
			q.Score += 25
		}
	}
}

func (s *AccountTestService) RunPelicanProbe(ctx context.Context, accountID int64, model string) (*ScheduledTestResult, error) {
	ctx, cancel := context.WithTimeout(ctx, 180*time.Second)
	defer cancel()
	a, err := s.accountRepo.GetByID(ctx, accountID)
	if err != nil {
		return nil, err
	}
	if a.Platform != PlatformOpenAI || a.IsSyntheticUITest() {
		now := time.Now()
		r := &ScheduledTestResult{TestKind: ScheduledTestPelicanV1, ModelID: model, Status: "failed", ErrorMessage: "Pelican probe currently requires an OpenAI account", StartedAt: now, FinishedAt: now}
		EvaluatePelicanProbe(r)
		return r, nil
	}
	r, err := s.runTestBackground(ctx, accountID, model, PelicanPrompt, true)
	if err != nil {
		return nil, err
	}
	r.TestKind = ScheduledTestPelicanV1
	EvaluatePelicanProbe(r)
	return r, nil
}
