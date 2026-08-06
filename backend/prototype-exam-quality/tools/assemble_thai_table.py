"""Assemble the Thai subject-tab/comparison-table HTML by reusing already
translated per-difficulty fragments (easy/medium/hard/calc), which were
translated while the report was still organized as difficulty-primary tabs.

Since the underlying question text never changed (only the DOM grouping
did), we can splice the already-translated <div class="qrow">... blobs into
the new table cells instead of re-translating ~300KB of content. We use the
untouched ENGLISH per-bucket fragment (still on disk) as the positional index
of truth: it tells us, in order, which skill category (Recall/Understanding/
Application/Analysis/Calculation) each <div class="cat"> block in the
matching Thai fragment corresponds to, since agents were instructed never to
reorder/add/remove elements.
"""
import os
import re
import sys

SPLIT_DIR = os.path.dirname(os.path.abspath(__file__)) + r"\liveverify_split"
TOOLS_DIR = r"E:\contribute\teach-me-all\backend\prototype-exam-quality\tools"
sys.path.insert(0, TOOLS_DIR)
import render_liveverify_html as gen  # noqa: E402

BUCKETS = gen.BUCKETS  # [("easy","Easy"), ("medium","Medium"), ("hard","Hard"), ("calc","Calculation")]
SUBJECTS = gen.SUBJECTS  # [(key, label, pages, paths), ...]

THAI_SUBJECT = {
    "Physics": "ฟิสิกส์", "Chemistry": "เคมี", "Economics": "เศรษฐศาสตร์",
    "US History": "ประวัติศาสตร์สหรัฐฯ", "Biology": "ชีววิทยา",
}
THAI_BUCKET = {"Easy": "ง่าย", "Medium": "ปานกลาง", "Hard": "ยาก", "Calculation": "การคำนวณ"}
THAI_ROW = {
    "Recall": "จำ", "Understanding": "เข้าใจ", "Application": "ประยุกต์ใช้",
    "Analysis": "วิเคราะห์", "Calculation": "การคำนวณ",
}

BUCKET_HEAD_RE = re.compile(r'<div class="bucket-head">(.*?)</div>')
CAT_START = '<div class="cat">'
CAT_HEAD_RE = re.compile(r'<div class="cat-head">.*?</span></div>', re.S)


def split_subject_segments(html_text):
    """Return list of (subject_label_text, segment_html) using bucket-head markers."""
    heads = list(BUCKET_HEAD_RE.finditer(html_text))
    segs = []
    for i, m in enumerate(heads):
        start = m.end()
        end = heads[i + 1].start() if i + 1 < len(heads) else len(html_text)
        segs.append((m.group(1).strip(), html_text[start:end]))
    return segs


def split_cat_blocks(segment_html):
    """Return list of (row_label_or_None, qrows_html) for a subject segment.

    row_label is filled in by the caller using the EN cat-head text; here we
    just split on <div class="cat"> boundaries and strip the cat-head div.
    """
    starts = [mm.start() for mm in re.finditer(re.escape(CAT_START), segment_html)]
    blocks = []
    for i, s in enumerate(starts):
        e = starts[i + 1] if i + 1 < len(starts) else len(segment_html)
        block = segment_html[s:e]
        head_m = CAT_HEAD_RE.search(block)
        qrows = block[head_m.end():] if head_m else block
        # each block ends with a trailing "</div>" that closes the .cat wrapper; strip it.
        qrows = qrows.rstrip()
        if qrows.endswith("</div>"):
            qrows = qrows[: -len("</div>")]
        blocks.append(qrows)
    return blocks


def cat_head_label(segment_html_en, index):
    starts = [mm.start() for mm in re.finditer(re.escape(CAT_START), segment_html_en)]
    s = starts[index]
    e = starts[index + 1] if index + 1 < len(starts) else len(segment_html_en)
    block = segment_html_en[s:e]
    m = re.search(r'<div class="cat-head">(.*?)\s*<span', block, re.S)
    return m.group(1).strip() if m else None


def build_lookup():
    """(subject_key, bucket, row_label) -> qrows_html (Thai)."""
    lookup = {}
    key_by_label = {label: key for key, label, _pages, _paths in SUBJECTS}
    for bucket, _bucket_label in BUCKETS:
        en_path = os.path.join(SPLIT_DIR, f"panel_tab-{bucket}.html")
        th_path = os.path.join(SPLIT_DIR, f"panel_tab-{bucket}_th.html")
        with open(en_path, encoding="utf-8") as f:
            en = f.read()
        with open(th_path, encoding="utf-8") as f:
            th = f.read()
        en_segs = split_subject_segments(en)
        th_segs = split_subject_segments(th)
        if len(en_segs) != len(th_segs):
            print(f"  [warn] {bucket}: EN has {len(en_segs)} subject segments, TH has {len(th_segs)}", file=sys.stderr)
        for idx, (en_label, en_seg_html) in enumerate(en_segs):
            subject_key = key_by_label.get(en_label)
            if subject_key is None:
                print(f"  [warn] {bucket}: unrecognized EN subject label {en_label!r}", file=sys.stderr)
                continue
            th_label, th_seg_html = th_segs[idx]
            th_blocks = split_cat_blocks(th_seg_html)
            n_cats = len(re.findall(re.escape(CAT_START), en_seg_html))
            if len(th_blocks) != n_cats:
                print(f"  [warn] {bucket}/{en_label}: EN has {n_cats} cats, TH has {len(th_blocks)}", file=sys.stderr)
            for i in range(min(n_cats, len(th_blocks))):
                row_label = cat_head_label(en_seg_html, i)
                if row_label is None:
                    continue
                lookup[(subject_key, bucket, row_label)] = th_blocks[i]
    return lookup


def render_cell_thai(info, qrows_th):
    if not info or qrows_th is None:
        return '<td class="empty">—</td>'
    return f'<td><div class="cell-score">{info["accepted"]}/{info["drafts"]}</div>{qrows_th}</td>'


def render_subject_tab_thai(key, label, pages, data, lookup):
    body = []
    strip = []
    for bucket, bucket_label in BUCKETS:
        total = 0
        accepted = 0
        for cat_key, cat_diff in gen.CATEGORY_DIFF.items():
            if cat_diff != bucket:
                continue
            info = data.get(cat_key)
            if not info:
                continue
            total += info["drafts"]
            accepted += info["accepted"]
        cls = "good" if total > 0 and accepted == total else ("warn" if total > 0 else "empty")
        strip.append(
            f'<div class="strip-cell {cls}"><div class="strip-label">{THAI_BUCKET[bucket_label]}</div>'
            f'<div class="strip-score">{accepted}/{total}</div></div>'
        )
    body.append(f'<div class="strip">{"".join(strip)}</div>')

    header_cells = "".join(f"<th>{THAI_BUCKET[bl]}</th>" for _b, bl in BUCKETS)
    body_rows = []
    for row_label in gen.L["rows"]:
        cells = []
        has_any = False
        for bucket, _bucket_label in BUCKETS:
            cat_key = gen.CELL_CAT.get((row_label, bucket))
            info = data.get(cat_key) if cat_key else None
            qrows_th = lookup.get((key, bucket, row_label))
            if info:
                has_any = True
            cells.append(render_cell_thai(info, qrows_th))
        if not has_any:
            continue
        body_rows.append(f"<tr><th>{THAI_ROW[row_label]}</th>{''.join(cells)}</tr>")

    table = (
        '<div class="qtable-wrap"><table class="qtable">'
        f'<thead><tr><th></th>{header_cells}</tr></thead>'
        f'<tbody>{"".join(body_rows)}</tbody>'
        '</table></div>'
    )
    body.append(table)

    tabid = f"tab-{key}"
    thai_label = THAI_SUBJECT[label]
    tabs_html = (
        f'<section class="tabpanel" id="{tabid}">'
        f'<div class="tab-meta">{thai_label} · {gen.esc(pages)}</div>'
        + "".join(body)
        + "</section>"
    )
    return tabid, thai_label, tabs_html


def build():
    print("Loading reports...")
    data = {}
    for key, label, pages, paths in SUBJECTS:
        data[key] = gen.load_subject(paths)

    print("Building reuse lookup from translated difficulty fragments...")
    lookup = build_lookup()
    print(f"  {len(lookup)} (subject,bucket,row) cells recovered")

    tab_buttons = []
    tab_panels = []
    for i, (key, label, pages, _paths) in enumerate(SUBJECTS):
        tabid, tlabel, panel = render_subject_tab_thai(key, label, pages, data[key], lookup)
        active = ' class="tabbtn active"' if i == 0 else ""
        tab_buttons.append(f'<button{active} data-tab="{tabid}">{tlabel}</button>')
        tab_panels.append(panel)

    with open(os.path.join(SPLIT_DIR, "header_th.html"), encoding="utf-8") as f:
        header = f.read()
    with open(os.path.join(SPLIT_DIR, "footer.html"), encoding="utf-8") as f:
        footer = f.read()

    doc = header + "\n" + "".join(tab_panels) + footer
    out = os.path.join(TOOLS_DIR, "liveverify-all-subjects.th.html")
    with open(out, "w", encoding="utf-8") as fh:
        fh.write(doc)
    print(f"wrote {out}")


if __name__ == "__main__":
    build()
