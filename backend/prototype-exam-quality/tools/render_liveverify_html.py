#!/usr/bin/env python3
"""Render live-verify benchmark results into a tabbed comparison HTML.

Layout: one tab per subject. Each skill gets its own table whose rows are
questions and whose columns are difficulty (Easy / Medium / Hard), so a
question and its harder/easier counterparts sit in the same row across
columns. Calculation is rendered as a separate section below the skill
tables, never inside them.
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

# category -> difficulty bucket (column axis)
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

# Difficulty bucket -> (english label, thai label)
BUCKET_LABELS = {
    "easy": ("Easy", "ง่าย"),
    "medium": ("Medium", "ปานกลาง"),
    "hard": ("Hard", "ยาก"),
    "calc": ("Calculation", "การคำนวณ"),
}
BUCKETS = [("easy", "Easy"), ("medium", "Medium"), ("hard", "Hard"), ("calc", "Calculation")]

# category -> (english label, thai label)
CATEGORY_LABELS = {
    "recall": ("Recall", "ความจำ"),
    "understanding": ("Understanding", "ความเข้าใจ"),
    "application": ("Application", "การประยุกต์"),
    "analysis": ("Analysis", "การวิเคราะห์"),
    "calculation": ("Calculation", "การคำนวณ"),
}

# Map every CATEGORY_DIFF key to its display group. Recall/understanding have
# only one difficulty; application/analysis repeat across easy/medium/hard.
def full_category_key(cat_key):
    return cat_key.split("-")[0]  # application-easy -> application

# (row label, bucket) -> cat_key. Labels depend on the active language, so it
# is rebuilt by set_lang() below.
CELL_CAT = {}

# active language strings; replaced wholesale by set_lang()
L = {
    "lang": "en",
    "title": "Live-verify · cross-subject exam questions",
    "sub": (
        "DeepSeek · candidate 3 · parallel 1 · set-generation. One tab per subject. "
        "Each skill gets its own table: rows are questions, columns are difficulty "
        "(Easy / Medium / Hard), so questions of the same index sit side by side "
        "across levels. Calculation is a separate section below, kept out of the "
        "skill tables."
    ),
    "subject_label": {},  # subject id -> label, filled below
    "bucket": {},         # bucket -> label
    "category": {},       # category -> row label
    "rows": [],           # skill row labels in order
    "pass": "PASS",
    "fail": "FAIL",
    "calc": "calc",
    "calc_label": "calc:",
    "skill_col": "Skill",
    "failed_of": "failed of {n} drafts",
    "failed_summary": "{n} failed of {d} drafts",
    "calc_section": "Calculation",
}

EN_SUBJECT_LABELS = {
    "physics": "Physics", "chemistry": "Chemistry", "economics": "Economics",
    "us-history": "US History", "biology": "Biology",
}
TH_SUBJECT_LABELS = {
    "physics": "ฟิสิกส์", "chemistry": "เคมี", "economics": "เศรษฐศาสตร์",
    "us-history": "ประวัติศาสตร์สหรัฐฯ", "biology": "ชีววิทยา",
}


def set_lang(code):
    """Switch the active language ('en' or 'th') for all rendered strings."""
    global L, BUCKETS, CELL_CAT
    thai = code == "th"
    if thai:
        L = {
            "lang": "th",
            "title": "ตรวจสอบสด · คำถามข้ามวิชา",
            "sub": (
                "DeepSeek · candidate 3 · parallel 1 · set-generation. หนึ่งแท็บต่อวิชา "
                "แต่ละทักษะมีตารางของตัวเอง: แถวคือข้อคำถาม คอลัมน์คือระดับความยาก "
                "(ง่าย / ปานกลาง / ยาก) ข้อลำดับเดียวกันจึงอยู่แถวเดียวกันข้ามระดับ "
                "ส่วนการคำนวณแยกเป็นส่วนต่างหากด้านล่าง ไม่รวมในตารางทักษะ"
            ),
            "subject_label": dict(TH_SUBJECT_LABELS),
            "bucket": {k: v[1] for k, v in BUCKET_LABELS.items()},
            "category": {
                "recall": "ความจำ", "understanding": "ความเข้าใจ",
                "application": "การประยุกต์", "analysis": "การวิเคราะห์",
                "calculation": "การคำนวณ",
            },
            "rows": ["ความจำ", "ความเข้าใจ", "การประยุกต์", "การวิเคราะห์", "การคำนวณ"],
            "pass": "ผ่าน",
            "fail": "ไม่ผ่าน",
            "calc": "คำนวณ",
            "calc_label": "คำนวณ:",
            "skill_col": "ทักษะ",
            "failed_of": "ล้ม {n} จาก {d} ฉบับ",
            "failed_summary": "{n} ไม่ผ่านจาก {d} ฉบับ",
            "calc_section": "การคำนวณ",
        }
        BUCKETS = [(k, v[1]) for k, v in BUCKET_LABELS.items()]
    else:
        L = {
            "lang": "en",
            "title": "Live-verify · cross-subject exam questions",
            "sub": (
                "DeepSeek · candidate 3 · parallel 1 · set-generation. One tab per subject. "
                "Each skill gets its own table: rows are questions, columns are difficulty "
                "(Easy / Medium / Hard), so questions of the same index sit side by side "
                "across levels. Calculation is a separate section below, kept out of the "
                "skill tables."
            ),
            "subject_label": dict(EN_SUBJECT_LABELS),
            "bucket": {k: v[0] for k, v in BUCKET_LABELS.items()},
            "category": {
                "recall": "Recall", "understanding": "Understanding",
                "application": "Application", "analysis": "Analysis",
                "calculation": "Calculation",
            },
            "rows": ["Recall", "Understanding", "Application", "Analysis", "Calculation"],
            "pass": "PASS",
            "fail": "FAIL",
            "calc": "calc",
            "calc_label": "calc:",
            "skill_col": "Skill",
            "failed_of": "failed of {n} drafts",
            "failed_summary": "{n} failed of {d} drafts",
            "calc_section": "Calculation",
        }
        BUCKETS = [(k, v[0]) for k, v in BUCKET_LABELS.items()]
    CELL_CAT = {}
    for _cat_key in CATEGORY_DIFF:
        row_label = L["category"][full_category_key(_cat_key)]
        CELL_CAT[(row_label, CATEGORY_DIFF[_cat_key])] = _cat_key


set_lang("en")


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
        calc = f'<div class="calc"><b>{esc(L["calc_label"])}</b> {esc(p.get("expression",""))} = {esc(p.get("expected",""))} {esc(p.get("unit",""))}</div>'
    fail = ""
    if not passed and fail_reasons:
        fail = '<div class="fail">' + "<br>".join(esc(r) for r in fail_reasons) + "</div>"
    status = '<span class="ok">' + esc(L["pass"]) + '</span>' if passed else '<span class="nok">' + esc(L["fail"]) + '</span>'
    # skill + calc are badges on the question, not table axes.
    skill = q.get("skill", "")
    skill_label = L["category"].get(skill, skill)
    badges = ""
    if skill_label:
        badges += f'<span class="badge skill">{esc(skill_label)}</span>'
    if q.get("requires_calculation") or q.get("calculation"):
        badges += f'<span class="badge calcflag">{esc(L["calc"])}</span>'
    return (
        f'<div class="qrow {"passed" if passed else "failed"}">'
        f'<div class="qmain">'
        f'<div class="qhead"><div class="qbadges">{badges}{status}</div><span class="qno">{qno}</span></div>'
        f'<div class="stem">{esc(q.get("stem",""))}</div>'
        + "".join(choices)
        + calc
        + fail
        + "</div></div>"
    )


def render_difficulty_column(bucket_label, cat_keys, data):
    """One difficulty column holds every question of that level, any skill."""
    questions = []
    for cat_key in cat_keys:
        info = data.get(cat_key)
        if not info:
            continue
        questions.extend(info["questions"])
    if not questions:
        return ""
    passed = [(q, p, r) for q, p, r in questions if p]
    failed = [(q, p, r) for q, p, r in questions if not p]
    accepted = len(passed)
    drafts = len(questions)
    rows = ""
    qno = 0
    for q, p, r in passed:
        qno += 1
        rows += render_question_row(q, p, r, qno)
    if failed:
        summary = L["failed_summary"].format(n=len(failed), d=drafts)
        rows += f'<details class="failed-q"><summary>{esc(summary)}</summary>'
        fqno = 0
        for q, p, r in failed:
            fqno += 1
            rows += render_question_row(q, p, r, fqno)
        rows += "</details>"
    cls = "good" if drafts > 0 and accepted == drafts else "warn"
    return (
        f'<div class="diff-col {cls}">'
        f'<div class="diff-head">{esc(bucket_label)} <span class="cell-score">{accepted}/{drafts}</span></div>'
        f'<div class="diff-body">{rows}</div>'
        f'</div>'
    )


def render_skill_matrix(skill_label, data):
    """One table per skill: rows = question index, columns = difficulty.

    Questions that passed sit in the row for their index; failed drafts are
    collapsed under the whole table so the grid stays aligned.
    """
    diff_buckets = [b for b in BUCKETS if b[0] != "calc"]
    # cat_key for this skill at each difficulty
    per_bucket = {}
    max_passed = 0
    for bucket, _bl in diff_buckets:
        cat_key = CELL_CAT.get((skill_label, bucket))
        info = data.get(cat_key) if cat_key else None
        passed = [(q, p, r) for q, p, r in info["questions"] if p] if info else []
        failed = [(q, p, r) for q, p, r in info["questions"] if not p] if info else []
        accepted = len(passed)
        drafts = len(passed) + len(failed)
        per_bucket[bucket] = {
            "passed": passed,
            "failed": failed,
            "accepted": accepted,
            "drafts": drafts,
        }
        max_passed = max(max_passed, len(passed))

    if max_passed == 0 and not any(pb["drafts"] for pb in per_bucket.values()):
        return ""

    header = "".join(
        f'<th>{esc(bl)} <span class="cell-score">{per_bucket[b]["accepted"]}/{per_bucket[b]["drafts"]}</span></th>'
        for b, bl in diff_buckets
    )
    rows_html = []
    for i in range(max_passed):
        cells = []
        for bucket, _bl in diff_buckets:
            pb = per_bucket[bucket]
            if i < len(pb["passed"]):
                cells.append(f'<td>{render_question_row(*pb["passed"][i], i + 1)}</td>')
            else:
                cells.append('<td class="empty">—</td>')
        rows_html.append(f"<tr><th>{i + 1}</th>{''.join(cells)}</tr>")
    # collapse failed drafts per column under the table
    failed_row = []
    for bucket, _bl in diff_buckets:
        pb = per_bucket[bucket]
        if pb["failed"]:
            body = "".join(
                render_question_row(q, p, r, i + 1) for i, (q, p, r) in enumerate(pb["failed"])
            )
            summary = L["failed_summary"].format(n=len(pb["failed"]), d=pb["drafts"])
            failed_row.append(
                f'<td><details class="failed-q"><summary>{esc(summary)}</summary>{body}</details></td>'
            )
        else:
            failed_row.append("<td></td>")
    if any(fr for fr in failed_row):
        rows_html.append(f"<tr><th>✗</th>{''.join(failed_row)}</tr>")

    return (
        f'<div class="skill-table-wrap"><h3 class="skill-head">{esc(skill_label)}</h3>'
        f'<table class="qtable"><thead><tr><th>#</th>{header}</tr></thead>'
        f'<tbody>{"".join(rows_html)}</tbody></table></div>'
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

    # One table per skill, rows = question, columns = difficulty.
    for skill_label in L["rows"]:
        if skill_label == L["category"]["calculation"]:
            continue  # calculation gets its own separate section
        table = render_skill_matrix(skill_label, data)
        if table:
            body.append(table)

    # Separate calculation section (kept out of every skill table).
    calc_cat_key = CELL_CAT.get((L["category"]["calculation"], "calc"))
    calc_info = data.get(calc_cat_key) if calc_cat_key else None
    if calc_info:
        col = render_difficulty_column(L["calc_section"], [calc_cat_key], data)
        body.append(f'<h2 class="calc-section">{esc(L["calc_section"])}</h2>')
        body.append(f'<div class="diff-grid">{"".join([col])}</div>')

    tabid = f"tab-{key}"
    tabs_html = (
        f'<section class="tabpanel" id="{tabid}">'
        f'<div class="tab-meta">{esc(label)} · {esc(pages)}</div>'
        + "".join(body)
        + "</section>"
    )
    return tabid, label, tabs_html


def build(lang="en"):
    set_lang(lang)
    data = {}
    print(f"Loading reports ({lang})...")
    for key, en_label, pages, paths in SUBJECTS:
        label = L["subject_label"][key]
        print(f"  {label}")
        data[key] = load_subject(paths)

    tab_buttons = []
    tab_panels = []
    for i, (key, en_label, pages, _paths) in enumerate(SUBJECTS):
        tabid, tlabel, panel = render_subject_tab(key, L["subject_label"][key], pages, data[key])
        # No active class in the static HTML: the runtime tab script drives
        # the active state with inline styles, so the CSS .tabbtn.active rule
        # never matches and no button can get stuck active.
        tab_buttons.append(f'<button class="tabbtn" data-tab="{tabid}">{esc(tlabel)}</button>')
        tab_panels.append(panel)

    doc = f"""<!DOCTYPE html>
<html lang="{L["lang"]}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{esc(L["title"])}</title>
<style>
:root {{ color-scheme: light dark; }}
body {{ font-family: system-ui, -apple-system, "Segoe UI", Roboto, sans-serif; margin: 1.5rem; line-height: 1.45; background: #fff; }}
h1 {{ font-size: 1.35rem; margin-bottom: .25rem; }}
.sub {{ color: #888; margin-bottom: 1.2rem; font-size: .9rem; }}
.tabs {{ display: flex; gap: .4rem; flex-wrap: wrap; margin-bottom: 1rem; position: sticky; top: 0; background: #fff; padding: .3rem 0; z-index: 5; }}
.tabbtn {{ padding: .45rem .9rem; border: 1px solid #4444; border-radius: 8px; background: #0000000d; cursor: pointer; font-weight: 600; font-size: .9rem; }}
.tabpanel {{ display: none; }}
.tab-meta {{ color: #888; font-size: .85rem; margin-bottom: .8rem; }}
.strip {{ display: flex; gap: .6rem; margin-bottom: 1.1rem; flex-wrap: wrap; }}
.strip-cell {{ border: 1px solid #4444; border-radius: 8px; padding: .4rem .8rem; min-width: 6rem; text-align: center; }}
.strip-label {{ font-size: .7rem; text-transform: uppercase; letter-spacing: .06em; color: #888; }}
.strip-score {{ font-size: 1.05rem; font-weight: 700; }}
.strip-cell.good .strip-score {{ color: #2e7d32; }}
.strip-cell.warn .strip-score {{ color: #c62828; }}
.strip-cell.empty {{ opacity: .5; }}
.skill-table-wrap {{ margin-bottom: 1.4rem; }}
.skill-head {{ font-size: .9rem; font-weight: 700; margin: .2rem 0 .45rem; }}
.qtable {{ border-collapse: collapse; width: 100%; table-layout: fixed; }}
.qtable th, .qtable td {{ border: 1px solid #4444; vertical-align: top; padding: .45rem .55rem; }}
.qtable thead th {{ background: #0000000d; font-size: .76rem; text-transform: uppercase; letter-spacing: .06em; color: #666; }}
.qtable thead th:first-child, .qtable tbody th {{ width: 2.2rem; text-align: center; color: #888; font-size: .78rem; background: #00000008; }}
.qtable td.empty {{ text-align: center; color: #aaa; font-style: italic; }}
.qtable .qrow {{ margin: 0; }}
.calc-section {{ font-size: .9rem; font-weight: 700; margin: 1.6rem 0 .5rem; border-top: 2px solid #4444; padding-top: .8rem; }}
.diff-grid {{ display: grid; grid-template-columns: repeat(auto-fit, minmax(240px, 1fr)); gap: .8rem; align-items: start; }}
.diff-col {{ border: 1px solid #4444; border-radius: 8px; overflow: hidden; display: flex; flex-direction: column; }}
.diff-col.warn {{ border-top: 3px solid #c62828; }}
.diff-col.good {{ border-top: 3px solid #2e7d32; }}
.diff-head {{ font-size: .78rem; font-weight: 700; text-transform: uppercase; letter-spacing: .08em; color: #666; padding: .5rem .65rem; background: #0000000d; display: flex; justify-content: space-between; align-items: center; }}
.diff-body {{ display: flex; flex-direction: column; gap: .45rem; padding: .65rem; }}
.cell-score {{ font-size: .72rem; font-weight: 700; color: #888; }}
.qrow {{ border: 1px solid #4444; border-radius: 6px; padding: .45rem .6rem; }}
.qrow.passed {{ border-left: 4px solid #2e7d32; }}
.qrow.failed {{ border-left: 4px solid #c62828; opacity: .85; }}
.qhead {{ display: flex; gap: .4rem; align-items: center; justify-content: space-between; margin-bottom: .3rem; }}
.qbadges {{ display: flex; gap: .3rem; align-items: center; flex-wrap: wrap; }}
.qno {{ font-size: .72rem; color: #888; }}
.qmain {{ flex: 1; }}
.ok {{ color: #2e7d32; font-weight: 700; font-size: .72rem; }}
.nok {{ color: #c62828; font-weight: 700; font-size: .72rem; }}
.stem {{ font-weight: 500; margin-bottom: .3rem; }}
.choice {{ font-size: .87rem; padding: .06rem .25rem; }}
.choice-correct {{ font-size: .87rem; padding: .06rem .25rem; background: #2e7d321f; border-radius: 3px; font-weight: 600; }}
.calc {{ font-size: .78rem; color: #777; margin-top: .25rem; }}
.fail {{ font-size: .74rem; color: #c62828; margin-top: .25rem; }}
.badge {{ font-size: .66rem; padding: .05rem .4rem; border-radius: 99px; border: 1px solid #4444; }}
.skill {{ background: #44444422; }}
.calcflag {{ background: #1565c033; color: #1565c0; }}
.failed-q {{ margin-top: .2rem; }}
.failed-q summary {{ cursor: pointer; font-size: .75rem; color: #c62828; }}
@media (prefers-color-scheme: dark) {{
  body {{ background: #111; }}
  .tabs {{ background: #111; }}
  .ok {{ color: #81c784; }} .nok, .fail {{ color: #ef9a9a; }}
  .strip-cell.good .strip-score {{ color: #81c784; }} .strip-cell.warn .strip-score {{ color: #ef9a9a; }}
  .choice-correct {{ background: #2e7d3255; }}
}}
</style>
</head>
<body>
<h1>{esc(L["title"])}</h1>
<div class="sub">{esc(L["sub"])}</div>
<div class="tabs">{''.join(tab_buttons)}</div>
{''.join(tab_panels)}
<script>
(function () {{
  var tabs = document.querySelector('.tabs');
  if (!tabs) return;
  // Collect tab buttons from the container's children and panels by walking
  // the DOM, NOT via querySelectorAll('.tabbtn'), and drive visibility with
  // INLINE STYLES instead of toggling classes. Some embedded renderers
  // corrupt class attributes when classList.add/remove is used (they wipe
  // the whole className), so we never touch className at runtime.
  var btns = [];
  for (var i = 0; i < tabs.children.length; i++) btns.push(tabs.children[i]);
  var panels = [];
  var all = document.body.getElementsByTagName('*');
  for (var j = 0; j < all.length; j++) {{
    var cls = all[j].className;
    if (typeof cls === 'string' && cls.indexOf('tabpanel') !== -1) panels.push(all[j]);
  }}
  var ACTIVE_BG = '#1976d2', ACTIVE_FG = '#fff';
  if (window.matchMedia && window.matchMedia('(prefers-color-scheme: dark)').matches) {{
    ACTIVE_BG = '#90caf9'; ACTIVE_FG = '#000';
  }}
  function activate(id) {{
    for (var k = 0; k < btns.length; k++) {{
      var is = btns[k].getAttribute('data-tab') === id;
      btns[k].style.background = is ? ACTIVE_BG : '';
      btns[k].style.color = is ? ACTIVE_FG : '';
    }}
    for (var m = 0; m < panels.length; m++) {{
      panels[m].style.display = (panels[m].id === id) ? 'block' : 'none';
    }}
  }}
  tabs.addEventListener('click', function (e) {{
    var t = e.target;
    while (t && t !== tabs) {{
      if (t.getAttribute && t.getAttribute('data-tab')) break;
      t = t.parentNode;
    }}
    if (t && t.getAttribute && t.getAttribute('data-tab')) activate(t.getAttribute('data-tab'));
  }});
  if (btns.length > 0) activate(btns[0].getAttribute('data-tab'));
}})();
</script>
</body>
</html>"""
    suffix = ".th.html" if L["lang"] == "th" else ".html"
    out = os.path.join(HERE, "liveverify-all-subjects" + suffix)
    with open(out, "w", encoding="utf-8") as fh:
        fh.write(doc)
    print(f"wrote {out}")


if __name__ == "__main__":
    build("en")
    build("th")
