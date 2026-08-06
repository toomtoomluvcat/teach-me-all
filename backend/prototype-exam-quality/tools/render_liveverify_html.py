#!/usr/bin/env python3
"""Render live-verify benchmark results into a tabbed comparison HTML.

Layout: one tab per subject. Inside a tab, questions are grouped by difficulty
(Easy / Medium / Hard / Calculation) as the primary section, and each question
is a row showing its skill, stem, choices, and gate result. This makes it easy
to compare questions of the same difficulty across subjects side by side by
switching tabs.
"""
import html
import json
import os
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.abspath(os.path.join(HERE, ".."))  # backend/prototype-exam-quality

# subject -> (id, label, pages, report paths). Later paths win for the same category.
SUBJECTS = [
    ("physics", "Physics", "Newton's Laws · 140–220", [
        ".scratch/e0312e3e107a1953/benchmark-all.json",
        ".scratch/e0312e3e107a1953/benchmark-applicationhard.json",
    ]),
    ("chemistry", "Chemistry", "Titration · 200–280", [
        ".scratch/a4508f0287624222/benchmark-calculation.json",
    ]),
    ("economics", "Economics", "Elasticity · 60–150", [
        ".scratch/411dd75621fa43f9/benchmark-all.json",
    ]),
    ("us-history", "US History", "Westward · 460–489", [
        ".scratch/bc9cce4c7060c17e/benchmark-recallunderstandingapplicationeasyapplic.json",
        ".scratch/bc9cce4c7060c17e/benchmark-all.json",
    ]),
    ("biology", "Biology", "Cell Respiration · 210–237", [
        ".scratch/87040807ef7f73b8/benchmark-all.json",
    ]),
]

# category -> difficulty bucket (primary grouping)
CATEGORY_DIFF = {
    "recall": "easy",
    "understanding": "easy",
    "application-easy": "easy",
    "analysis-easy": "easy",
    "application-medium": "medium",
    "analysis-medium": "medium",
    "application-hard": "hard",
    "analysis-hard": "hard",
    "calculation": "calc",
}

BUCKETS = [("easy", "Easy"), ("medium", "Medium"), ("hard", "Hard"), ("calc", "Calculation")]
CATEGORY_LABEL = {
    "recall": "Recall", "understanding": "Understanding",
    "application-easy": "Application", "application-medium": "Application", "application-hard": "Application",
    "analysis-easy": "Analysis", "analysis-medium": "Analysis", "analysis-hard": "Analysis",
    "calculation": "Calculation",
}


def esc(s):
    return html.escape(str(s), quote=True)


def load_subject(report_paths):
    """Merge reports: category -> list of (question, passed, fail_reasons)."""
    merged = {}
    for path in report_paths:
        full = os.path.join(ROOT, path)
        if not os.path.exists(full):
            print(f"  [warn] missing report: {full}", file=sys.stderr)
            continue
        with open(full, encoding="utf-8") as fh:
            report = json.load(fh)
        for case in report.get("cases", []):
            name = case.get("name")
            if not name:
                continue
            questions = []
            for q in (case.get("questions") or []):
                passed = bool(q.get("passed"))
                gates = q.get("gates", q.get("Gates", []))
                fail_reasons = [
                    g.get("Reason") or g.get("reason")
                    for g in gates
                    if not (g.get("Pass") or g.get("pass"))
                ]
                questions.append((q, passed, [r for r in fail_reasons if r]))
            merged[name] = {
                "drafts": case.get("drafts", len(questions)),
                "accepted": case.get("accepted", 0),
                "questions": questions,
            }
    return merged


def render_question_row(q, passed, fail_reasons, qno):
    correct = None
    for i, c in enumerate(q.get("choices", [])):
        if c.get("is_correct"):
            correct = i
    choices = []
    for i, c in enumerate(q.get("choices", [])):
        content = c.get("content", "")
        tag = " ✓" if i == correct else ""
        choices.append(
            f'<div class="{"choice-correct" if i == correct else "choice"}">{esc(content)}{tag}</div>'
        )
    calc = ""
    if q.get("calculation"):
        p = q["calculation"]
        calc = f'<div class="calc"><b>calc:</b> {esc(p.get("expression",""))} = {esc(p.get("expected",""))} {esc(p.get("unit",""))}</div>'
    fail = ""
    if not passed and fail_reasons:
        fail = '<div class="fail">' + "<br>".join(esc(r) for r in fail_reasons) + "</div>"
    skill = esc(q.get("skill", ""))
    status = '<span class="ok">PASS</span>' if passed else '<span class="nok">FAIL</span>'
    return (
        f'<div class="qrow {"passed" if passed else "failed"}">'
        f'<div class="qnum">{qno}</div>'
        f'<div class="qmain">'
        f'<div class="qhead"><span class="badge skill">{skill}</span>{status}</div>'
        f'<div class="stem">{esc(q.get("stem",""))}</div>'
        + "".join(choices)
        + calc
        + fail
        + "</div></div>"
    )


def render_subject_tab(key, label, pages, data):
    body = []
    strip = []
    for bucket, bucket_label in BUCKETS:
        total = 0
        accepted = 0
        for cat_key, cat_diff in CATEGORY_DIFF.items():
            if cat_diff != bucket:
                continue
            info = data.get(cat_key)
            if not info:
                continue
            total += info["drafts"]
            accepted += info["accepted"]
        cls = "good" if total > 0 and accepted == total else ("warn" if total > 0 else "empty")
        strip.append(
            f'<div class="strip-cell {cls}"><div class="strip-label">{bucket_label}</div>'
            f'<div class="strip-score">{accepted}/{total}</div></div>'
        )
    body.append(f'<div class="strip">{"".join(strip)}</div>')

    for bucket, bucket_label in BUCKETS:
        cats = [(k, CATEGORY_LABEL[k]) for k, d in CATEGORY_DIFF.items() if d == bucket]
        has = any(cat in data for cat, _ in cats)
        if not has:
            continue
        body.append(f'<div class="bucket-head">{bucket_label}</div>')
        for cat_key, cat_label in cats:
            info = data.get(cat_key)
            if not info:
                continue
            passed = [(q, p, r) for q, p, r in info["questions"] if p]
            failed = [(q, p, r) for q, p, r in info["questions"] if not p]
            rows = ""
            qno = 0
            for q, p, r in passed:
                qno += 1
                rows += render_question_row(q, p, r, qno)
            if failed:
                rows += (
                    f'<details class="failed-q"><summary>{len(failed)} failed '
                    f'of {info["drafts"]} drafts</summary>'
                )
                fqno = 0
                for q, p, r in failed:
                    fqno += 1
                    rows += render_question_row(q, p, r, fqno)
                rows += "</details>"
            body.append(
                f'<div class="cat">'
                f'<div class="cat-head">{esc(cat_label)} <span class="cat-score">'
                f'{info["accepted"]}/{info["drafts"]}</span></div>'
                f'{rows}'
                f'</div>'
            )

    tabid = f"tab-{key}"
    tabs_html = (
        f'<section class="tabpanel" id="{tabid}">'
        f'<div class="tab-meta">{esc(label)} · {esc(pages)}</div>'
        + "".join(body)
        + "</section>"
    )
    return tabid, label, tabs_html


def build():
    data = {}
    print("Loading reports...")
    for key, label, pages, paths in SUBJECTS:
        print(f"  {label}")
        data[key] = load_subject(paths)

    tab_buttons = []
    tab_panels = []
    for i, (key, label, pages, _paths) in enumerate(SUBJECTS):
        tabid, tlabel, panel = render_subject_tab(key, label, pages, data[key])
        active = ' class="tabbtn active"' if i == 0 else ""
        tab_buttons.append(f'<button{active} data-tab="{tabid}">{esc(tlabel)}</button>')
        tab_panels.append(panel)

    doc = f"""<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Live-verify · cross-subject questions</title>
<style>
:root {{ color-scheme: light dark; }}
body {{ font-family: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; margin: 1.5rem; line-height: 1.45; }}
h1 {{ font-size: 1.35rem; margin-bottom: .25rem; }}
.sub {{ color: #888; margin-bottom: 1.2rem; font-size: .9rem; }}
.tabs {{ display: flex; gap: .4rem; flex-wrap: wrap; margin-bottom: 1rem; position: sticky; top: 0; background: inherit; padding: .3rem 0; z-index: 5; }}
.tabbtn {{ padding: .45rem .9rem; border: 1px solid #4444; border-radius: 8px; background: #0000000d; cursor: pointer; font-weight: 600; font-size: .9rem; }}
.tabbtn.active {{ background: #1976d2; color: #fff; border-color: #1976d2; }}
.tabpanel {{ display: none; }}
.tabpanel.active {{ display: block; }}
.tab-meta {{ color: #888; font-size: .85rem; margin-bottom: .8rem; }}
.strip {{ display: flex; gap: .6rem; margin-bottom: 1.1rem; flex-wrap: wrap; }}
.strip-cell {{ border: 1px solid #4444; border-radius: 8px; padding: .4rem .8rem; min-width: 6rem; text-align: center; }}
.strip-label {{ font-size: .7rem; text-transform: uppercase; letter-spacing: .06em; color: #888; }}
.strip-score {{ font-size: 1.05rem; font-weight: 700; }}
.strip-cell.good .strip-score {{ color: #2e7d32; }}
.strip-cell.warn .strip-score {{ color: #c62828; }}
.strip-cell.empty {{ opacity: .5; }}
.bucket-head {{ font-size: .78rem; font-weight: 700; text-transform: uppercase; letter-spacing: .1em; margin: 1.2rem 0 .4rem; color: #666; border-bottom: 1px solid #4444; padding-bottom: .25rem; }}
.cat {{ margin-bottom: .9rem; }}
.cat-head {{ font-weight: 600; font-size: .9rem; margin-bottom: .35rem; }}
.cat-score {{ font-size: .75rem; font-weight: 700; color: #888; margin-left: .4rem; }}
.qrow {{ display: flex; gap: .7rem; border: 1px solid #4444; border-radius: 6px; padding: .45rem .6rem; margin-bottom: .4rem; }}
.qrow.passed {{ border-left: 4px solid #2e7d32; }}
.qrow.failed {{ border-left: 4px solid #c62828; opacity: .85; }}
.qnum {{ font-size: .75rem; color: #888; min-width: 1.4rem; text-align: right; padding-top: .15rem; }}
.qmain {{ flex: 1; }}
.qhead {{ display: flex; gap: .4rem; align-items: center; margin-bottom: .25rem; }}
.ok {{ color: #2e7d32; font-weight: 700; font-size: .72rem; }}
.nok {{ color: #c62828; font-weight: 700; font-size: .72rem; }}
.stem {{ font-weight: 500; margin-bottom: .3rem; }}
.choice {{ font-size: .87rem; padding: .06rem .25rem; }}
.choice-correct {{ font-size: .87rem; padding: .06rem .25rem; background: #2e7d321f; border-radius: 3px; font-weight: 600; }}
.calc {{ font-size: .78rem; color: #777; margin-top: .25rem; }}
.fail {{ font-size: .74rem; color: #c62828; margin-top: .25rem; }}
.badge {{ font-size: .66rem; padding: .05rem .4rem; border-radius: 99px; border: 1px solid #4444; }}
.skill {{ background: #44444422; }}
.failed-q {{ margin-top: .2rem; }}
.failed-q summary {{ cursor: pointer; font-size: .75rem; color: #c62828; }}
@media (prefers-color-scheme: dark) {{
  .ok {{ color: #81c784; }} .nok, .fail {{ color: #ef9a9a; }}
  .strip-cell.good .strip-score {{ color: #81c784; }} .strip-cell.warn .strip-score {{ color: #ef9a9a; }}
  .choice-correct {{ background: #2e7d3255; }}
  .tabbtn.active {{ background: #90caf9; color: #000; }}
}}
</style>
</head>
<body>
<h1>Live-verify · cross-subject exam questions</h1>
<div class="sub">
DeepSeek · candidate 3 · parallel 1 · set-generation. One tab per subject.
Within each subject, questions are grouped by difficulty (Easy → Hard →
Calculation) and each question is a row. Switch tabs to compare the same
difficulty level across subjects.
</div>
<div class="tabs">{''.join(tab_buttons)}</div>
{''.join(tab_panels)}
<script>
(function () {{
  var tabs = document.querySelector('.tabs');
  if (!tabs) return;
  function activate(id) {{
    document.querySelectorAll('.tabbtn').forEach(function (b) {{ b.classList.remove('active'); }});
    document.querySelectorAll('.tabpanel').forEach(function (p) {{ p.classList.remove('active'); }});
    var btn = tabs.querySelector('[data-tab="' + id + '"]');
    if (btn) btn.classList.add('active');
    var panel = document.getElementById(id);
    if (panel) panel.classList.add('active');
  }}
  // Event delegation: one listener on the container catches clicks on any
  // button, so it keeps working even if individual buttons are re-created.
  tabs.addEventListener('click', function (e) {{
    var btn = e.target.closest('[data-tab]');
    if (btn) activate(btn.getAttribute('data-tab'));
  }});
  // Default to the first panel.
  var first = tabs.querySelector('.tabbtn');
  if (first) activate(first.getAttribute('data-tab'));
}})();
</script>
</body>
</html>"""
    out = os.path.join(HERE, "liveverify-all-subjects.html")
    with open(out, "w", encoding="utf-8") as fh:
        fh.write(doc)
    print(f"wrote {out}")


if __name__ == "__main__":
    build()
