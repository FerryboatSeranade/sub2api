# Pelican SVG scheduled probe v1

Admin accounts → Scheduled tests → Add plan → Pelican bicycle SVG (auxiliary).
Select an OpenAI model, a five-field cron schedule, and enable the plan. Manual
Run now uses the same account lock, worker limit and persistence as cron. The
existing vm2api transfer moves this plan and its history to the derived account.

Results retain model, time, latency, raw response, safe SVG and structural checks.
Checks (25 points each): restricted valid XML/SVG, positive viewBox, >=8 vector
shapes, and a pair of similar, aligned, separated circle candidates. The latter
is only a heuristic: paths/transforms/ellipses may draw valid wheels without
matching it; two unrelated circles can match it. No visual-quality or model
intelligence verdict is inferred. Network/quota failures remain inconclusive.
The probe never overwrites candy results, auto-excludes or auto-recovers accounts.

SVG is parsed with Go encoding/xml, bounded to 256 KiB / 5000 elements / 64 levels,
and checked against an element allowlist. External references, scripts, CSS
style elements, embedded images, event handlers and animation are disallowed.
Preview uses an img data URI, never v-html or an active SVG document.

References reviewed (no third-party code copied):
- https://github.com/simonw/pelican-bicycle — original prompt and examples.
- https://github.com/huggingface/OpenEnv/blob/main/docs/source/environments/pelican_svg.md
  — layered structural/rendered/visual judging; warns this task is saturated.
- https://github.com/pooriaarab/benchmark-pelican/blob/main/docs/grading.md
  — geometry rubric and limits of deterministic checks. Our small diagnostic
  checker is NOT that project's grader and its scores are not comparable.

Visual judging is deliberately not simulated by counting SVG elements. A future
version can add an independently configured vision judge and calibrated baseline.
