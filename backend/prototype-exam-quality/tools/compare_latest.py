#!/usr/bin/env python3
"""Build a readable, source-aware HTML comparison for an exam run.

The tool intentionally keeps the comparison visual rather than assigning an
automatic quality score. NotebookLM exports are parsed with the same strict
parser used by the blind-labelling workflow, while our JSON keeps provenance,
gate results, and explanations visible in expandable details.
"""

from __future__ import annotations

import argparse
import json
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
from labelkit import parse_notebooklm


def load_ours(path: Path) -> list[dict]:
    report = json.loads(path.read_text(encoding="utf-8"))
    rows: list[dict] = []
    for case in report.get("cases", []):
        for index, question in enumerate(case.get("questions", []), start=1):
            rows.append(
                {
                    "source": "ours",
                    "source_label": "Ours · latest Physics run",
                    "group": case.get("name", "run"),
                    "group_label": f"{case.get('name', 'run')} · {case.get('lesson', '')}",
                    "index": index,
                    "stem": question.get("stem", ""),
                    "choices": [c.get("content", "") for c in question.get("choices", [])],
                    "correct": next(
                        (i for i, c in enumerate(question.get("choices", [])) if c.get("is_correct")),
                        None,
                    ),
                    "passed": bool(question.get("passed")),
                    "skill": question.get("skill", ""),
                    "difficulty": question.get("difficulty", ""),
                    "explanation": question.get("explanation", ""),
                    "source_quote": question.get("source_quote", ""),
                    "coverage_slot_id": question.get("coverage_slot_id", ""),
                    "evidence_atom_id": question.get("evidence_atom_id", ""),
                    "evidence_chunk_id": question.get("evidence_chunk_id", ""),
                    "gates": question.get("gates", []),
                }
            )
    return rows


def load_nlm(path: Path, source: str, label: str) -> list[dict]:
    rows: list[dict] = []
    for index, question in enumerate(parse_notebooklm(path), start=1):
        rows.append(
            {
                "source": source,
                "source_label": label,
                "group": source,
                "group_label": label,
                "index": index,
                "stem": question.get("stem", ""),
                "choices": question.get("choices", []),
                "correct": question.get("correct"),
                "passed": None,
                "skill": "",
                "difficulty": "",
                "explanation": "",
                "source_quote": "",
                "coverage_slot_id": "",
                "evidence_atom_id": "",
                "evidence_chunk_id": "",
                "gates": [],
            }
        )
    return rows


def js_json(value: object) -> str:
    # The JSON is embedded in a script tag; closing-tag fragments must not be
    # allowed to terminate it when a source contains literal HTML.
    return json.dumps(value, ensure_ascii=False).replace("</", "<\\\\/")


def build_html(ours: list[dict], pages: list[dict], book: list[dict], metadata: dict) -> str:
    data = ours + pages + book
    meta_json = js_json(metadata)
    data_json = js_json(data)
    return f'''<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Exam comparison · latest run vs NotebookLM</title>
<style>
:root {{ color-scheme: light; --ink:#172033; --muted:#64748b; --line:#dbe2ea; --panel:#fff; --bg:#f4f7fb; --ours:#155eef; --pages:#9a6700; --book:#7c3aed; --good:#087443; --bad:#b42318; }}
* {{ box-sizing:border-box; }}
body {{ margin:0; background:var(--bg); color:var(--ink); font:15px/1.55 system-ui,-apple-system,"Segoe UI",sans-serif; }}
header {{ padding:28px max(20px,calc((100vw - 1400px)/2)); background:#101828; color:#fff; }}
h1 {{ margin:0 0 8px; font-size:25px; }}
h2 {{ margin:0; font-size:19px; }}
p {{ margin:7px 0; }}
.subtle {{ color:#b8c2d1; }}
.warning {{ max-width:1400px; margin:18px auto 0; padding:14px 18px; border:1px solid #f3c55b; border-radius:12px; background:#fff8df; color:#694b00; }}
.warning strong {{ display:block; margin-bottom:4px; }}
.toolbar {{ position:sticky; top:0; z-index:4; padding:12px max(20px,calc((100vw - 1400px)/2)); background:rgba(244,247,251,.95); border-bottom:1px solid var(--line); backdrop-filter:blur(8px); }}
.toolbar-inner {{ display:flex; flex-wrap:wrap; gap:8px; align-items:center; }}
button, input {{ font:inherit; }}
button {{ border:1px solid var(--line); border-radius:9px; padding:8px 12px; background:#fff; color:var(--ink); cursor:pointer; }}
button.active {{ background:#172033; color:#fff; border-color:#172033; }}
input {{ min-width:260px; flex:1; border:1px solid var(--line); border-radius:9px; padding:9px 12px; background:#fff; }}
main {{ max-width:1400px; margin:0 auto; padding:20px; }}
.summary {{ display:grid; grid-template-columns:repeat(auto-fit,minmax(180px,1fr)); gap:10px; margin-bottom:18px; }}
.metric {{ padding:13px 15px; background:var(--panel); border:1px solid var(--line); border-radius:12px; }}
.metric b {{ display:block; font-size:21px; }}
.metric span {{ color:var(--muted); font-size:13px; }}
.section {{ margin:22px 0 30px; }}
.section-title {{ display:flex; gap:10px; align-items:baseline; margin-bottom:10px; }}
.section-title small {{ color:var(--muted); }}
.grid {{ display:grid; grid-template-columns:repeat(auto-fit,minmax(330px,1fr)); gap:13px; }}
.card {{ background:var(--panel); border:1px solid var(--line); border-left:5px solid var(--line); border-radius:12px; padding:15px; box-shadow:0 2px 7px #1720330b; }}
.card.ours {{ border-left-color:var(--ours); }} .card.nlm-pages {{ border-left-color:var(--pages); }} .card.nlm-book {{ border-left-color:var(--book); }}
.card-head {{ display:flex; justify-content:space-between; gap:8px; align-items:start; margin-bottom:9px; }}
.badge {{ display:inline-block; padding:3px 8px; border-radius:999px; background:#eef2f7; color:var(--muted); font-size:12px; }}
.badge.ours {{ background:#e8f0ff; color:#1146a8; }} .badge.nlm-pages {{ background:#fff3cd; color:#795b00; }} .badge.nlm-book {{ background:#f0e9ff; color:#5b21b6; }}
.stem {{ font-weight:650; margin-bottom:10px; }}
.choices {{ display:grid; gap:5px; margin:0; padding:0; list-style:none; }}
.choice {{ padding:7px 9px; border:1px solid #e5eaf0; border-radius:8px; }}
.choice.correct {{ border-color:#8bd5ad; background:#effcf4; }}
.letter {{ display:inline-block; width:24px; color:var(--muted); font-weight:700; }}
.details {{ margin-top:12px; border-top:1px solid #eef1f5; padding-top:8px; }}
summary {{ cursor:pointer; color:#344054; font-weight:600; }}
.detail-grid {{ display:grid; grid-template-columns:150px 1fr; gap:5px 10px; margin-top:9px; font-size:13px; }}
.detail-grid dt {{ color:var(--muted); }} .detail-grid dd {{ margin:0; overflow-wrap:anywhere; }}
code {{ font-family:ui-monospace,SFMono-Regular,Consolas,monospace; font-size:12px; }}
.gate-fail {{ color:var(--bad); }} .gate-pass {{ color:var(--good); }}
.empty {{ padding:30px; color:var(--muted); text-align:center; background:#fff; border:1px dashed var(--line); border-radius:12px; }}
.matrix {{ overflow:auto; }}
table {{ width:100%; min-width:900px; border-collapse:separate; border-spacing:0 8px; }}
th {{ text-align:left; color:var(--muted); font-size:12px; padding:0 10px 3px; }}
td {{ vertical-align:top; width:33.3%; padding:12px; background:#fff; border-top:1px solid var(--line); border-bottom:1px solid var(--line); }}
td:first-child {{ border-left:1px solid var(--line); border-radius:10px 0 0 10px; }} td:last-child {{ border-right:1px solid var(--line); border-radius:0 10px 10px 0; }}
.matrix .stem {{ font-size:14px; }}
@media(max-width:650px) {{ .detail-grid {{ grid-template-columns:1fr; gap:1px; }} input {{ min-width:180px; }} }}
</style>
</head>
<body>
<header>
  <h1>Exam comparison · latest generated set vs NotebookLM</h1>
  <p class="subtle" id="meta"></p>
  <div class="warning"><strong>Important: source mismatch</strong>
    The latest generated set is Physics from <code>openstax-physics.pdf</code>.
    The available NotebookLM exports are the older Thai Biology baseline. Use
    this page for visual inspection only; it is not a valid quality win/loss
    comparison until NotebookLM is run on the same Physics source.
  </div>
</header>
<div class="toolbar"><div class="toolbar-inner">
  <button class="active" data-filter="matrix">Pair view</button>
  <button data-filter="ours">Ours</button>
  <button data-filter="nlm-pages">NLM · pages</button>
  <button data-filter="nlm-book">NLM · whole book</button>
  <button data-filter="all">All cards</button>
  <input id="search" placeholder="Search stem, choice, quote, or ID…">
</div></div>
<main>
  <div class="summary" id="summary"></div>
  <section id="content"></section>
</main>
<script>
const DATA = {data_json};
const META = {meta_json};
const letters = ["A", "B", "C", "D"];
const esc = value => String(value ?? "").replace(/[&<>\"']/g, ch => ({{"&":"&amp;","<":"&lt;",">":"&gt;",'\"':"&quot;", "'":"&#39;"}}[ch]));
document.getElementById("meta").textContent = `${{META.ours_path}} · ${{META.pages_path}} · ${{META.book_path}}`;

function card(q) {{
  const choices = (q.choices || []).map((choice, i) => `<li class="choice ${{q.correct === i ? "correct" : ""}}"><span class="letter">${{letters[i] || "?"}}.</span>${{esc(choice)}}</li>`).join("");
  const gates = (q.gates || []).map(g => `<div class="${{g.pass ? "gate-pass" : "gate-fail"}}">${{g.pass ? "✓" : "✗"}} ${{esc(g.gate)}} — ${{esc(g.reason)}}</div>`).join("");
  const status = q.source === "ours" ? `<span class="badge ${{q.source}}">${{q.passed ? "accepted" : "rejected"}}</span>` : "";
  const details = q.source === "ours" ? `<dl class="detail-grid">
    <dt>skill / difficulty</dt><dd>${{esc(q.skill)}} / ${{esc(q.difficulty)}}</dd>
    <dt>coverage slot</dt><dd><code>${{esc(q.coverage_slot_id)}}</code></dd>
    <dt>evidence atom</dt><dd><code>${{esc(q.evidence_atom_id)}}</code></dd>
    <dt>evidence chunk</dt><dd><code>${{esc(q.evidence_chunk_id)}}</code></dd>
    <dt>source quote</dt><dd>${{esc(q.source_quote)}}</dd>
    <dt>explanation</dt><dd>${{esc(q.explanation)}}</dd>
  </dl><div class="details-gates">${{gates}}</div>` : `<dl class="detail-grid"><dt>stated answer</dt><dd>${{q.correct == null ? "not stated" : letters[q.correct]}}</dd></dl>`;
  return `<article class="card ${{q.source}}"><div class="card-head"><div><span class="badge ${{q.source}}">${{esc(q.source_label)}}</span><div><small>#${{q.index}} · ${{esc(q.group_label)}}</small></div></div>${{status}}</div><div class="stem">${{esc(q.stem)}}</div><ul class="choices">${{choices}}</ul><details class="details"><summary>Evidence / QC details</summary>${{details}}</details></article>`;
}}

function matches(q, query) {{
  if (!query) return true;
  return JSON.stringify(q).toLowerCase().includes(query.toLowerCase());
}}

function summary() {{
  const ours = DATA.filter(q => q.source === "ours");
  const pages = DATA.filter(q => q.source === "nlm-pages");
  const book = DATA.filter(q => q.source === "nlm-book");
  document.getElementById("summary").innerHTML = [
    ["Ours", `${{ours.filter(q => q.passed).length}}/${{ours.length}} accepted`, "latest Physics set"],
    ["NLM · pages", `${{pages.length}} questions`, "legacy Biology export"],
    ["NLM · whole book", `${{book.length}} questions`, "legacy Biology export"],
    ["Comparison mode", "visual only", "run NLM again on Physics for a valid verdict"],
  ].map(([a,b,c]) => `<div class="metric"><span>${{a}}</span><b>${{b}}</b><span>${{c}}</span></div>`).join("");
}}

function matrix(query) {{
  const ours = DATA.filter(q => q.source === "ours");
  const pages = DATA.filter(q => q.source === "nlm-pages");
  const book = DATA.filter(q => q.source === "nlm-book");
  const max = Math.max(ours.length, pages.length, book.length);
  let rows = "";
  for (let i = 0; i < max; i++) {{
    const cells = [ours[i], pages[i], book[i]].map(q => q && matches(q, query) ? `<td>${{card(q)}}</td>` : `<td></td>`).join("");
    if (cells.replace(/<td><\\/td>/g, "").trim()) rows += `<tr>${{cells}}</tr>`;
  }}
  return `<div class="matrix"><table><thead><tr><th>Ours · latest Physics</th><th>NLM · pages · Biology legacy</th><th>NLM · whole book · Biology legacy</th></tr></thead><tbody>${{rows}}</tbody></table></div>`;
}}

function cards(source, query) {{
  const groups = [...new Set(DATA.filter(q => source === "all" || q.source === source).map(q => q.group))];
  return groups.map(group => {{
    const questions = DATA.filter(q => q.group === group && (source === "all" || q.source === source) && matches(q, query));
    if (!questions.length) return "";
    return `<section class="section"><div class="section-title"><h2>${{esc(questions[0].group_label)}}</h2><small>${{questions.length}} visible</small></div><div class="grid">${{questions.map(card).join("")}}</div></section>`;
  }}).join("") || `<div class="empty">No matching questions.</div>`;
}}

function render() {{
  const filter = document.querySelector("button.active").dataset.filter;
  const query = document.getElementById("search").value.trim();
  document.getElementById("content").innerHTML = filter === "matrix" ? matrix(query) : cards(filter, query);
}}
document.querySelectorAll("button[data-filter]").forEach(button => button.addEventListener("click", () => {{
  document.querySelectorAll("button[data-filter]").forEach(b => b.classList.remove("active"));
  button.classList.add("active"); render();
}}));
document.getElementById("search").addEventListener("input", render);
summary(); render();
</script>
</body>
</html>
'''


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ours", type=Path, required=True, help="benchmark JSON")
    parser.add_argument("--nlm-pages", type=Path, required=True)
    parser.add_argument("--nlm-book", type=Path, required=True)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()

    ours = load_ours(args.ours)
    pages = load_nlm(args.nlm_pages, "nlm-pages", "NotebookLM · pages")
    book = load_nlm(args.nlm_book, "nlm-book", "NotebookLM · whole book")
    report = json.loads(args.ours.read_text(encoding="utf-8"))
    metadata = {
        "ours_path": str(args.ours),
        "pages_path": str(args.nlm_pages),
        "book_path": str(args.nlm_book),
        "suite": report.get("suite", ""),
    }
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(build_html(ours, pages, book, metadata), encoding="utf-8")
    print(f"wrote {args.out} ({len(ours)} ours + {len(pages)} NLM pages + {len(book)} NLM book)")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
