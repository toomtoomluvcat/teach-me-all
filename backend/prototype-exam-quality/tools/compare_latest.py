#!/usr/bin/env python3
"""Build a Thai, source-aware HTML comparison for an exam run."""

from __future__ import annotations

import argparse
import html
import json
import sys
from pathlib import Path

HERE = Path(__file__).resolve().parent
sys.path.insert(0, str(HERE))
from labelkit import parse_notebooklm

CATEGORIES = (
    "การประยุกต์ใช้ · ง่าย",
    "การประยุกต์ใช้ · ยาก",
    "การคำนวณ",
)
CASE_LABELS = {
    "application-easy": "การประยุกต์ใช้ · ง่าย",
    "application-hard": "การประยุกต์ใช้ · ยาก",
    "calculation": "การคำนวณ",
}
SOURCE_STYLES = {
    "ours": ("#155eef", "#e8f0ff", "#1146a8"),
    "nlm-whole": ("#9a6700", "#fff3cd", "#795b00"),
    "nlm-partial": ("#00897b", "#e0f7f4", "#00695c"),
}
SKILL_TH = {"application": "ประยุกต์ใช้", "calculation": "คำนวณ"}
DIFFICULTY_TH = {"easy": "ง่าย", "medium": "ปานกลาง", "hard": "ยาก"}
GATE_TH = {
    "well_formed": "โครงสร้างข้อ",
    "source_role": "บทบาทแหล่งข้อมูล",
    "quote_verbatim": "คำอ้างอิงตรงต้นฉบับ",
    "arithmetic": "ตรวจเลขคณิต",
    "unit_check": "ตรวจหน่วย",
    "coverage_contract": "สัญญาความครอบคลุม",
    "not_a_duplicate": "ตรวจข้อซ้ำ",
}


OURS_THAI = [
    {
        "stem": "นักกระโดดร่มตกลงมาโดยแรงต้านอากาศมีค่าน้อยมาก แรงภายนอกลัพธ์ที่กระทำต่อนักกระโดดร่มคืออะไร?",
        "choices": [
            "มีเฉพาะแรงโน้มถ่วง (น้ำหนัก)",
            "แรงโน้มถ่วงรวมกับแรงต้านอากาศ",
            "เป็นศูนย์ เพราะนักกระโดดร่มกำลังตก",
            "มีเฉพาะแรงต้านอากาศ",
        ],
        "explanation": "เมื่อแรงต้านอากาศมีค่าน้อยมาก แรงเดียวที่กระทำคือ น้ำหนัก ดังนั้นแรงภายนอกลัพธ์จึงเท่ากับแรงโน้มถ่วง",
        "source_quote": "ถ้าแรงต้านอากาศมีค่าน้อยมาก แรงภายนอกลัพธ์ของวัตถุที่ตกลงมาจะมีเฉพาะแรงโน้มถ่วง หรือ น้ำหนักของวัตถุ",
    },
    {
        "stem": "มีแรงลัพธ์ขนาด 12 N กระทำต่อวัตถุมวล 3 kg ความเร่งของวัตถุมีค่าเท่าใด?",
        "choices": ["4 m/s²", "36 m/s²", "0.25 m/s²", "9 m/s²"],
        "explanation": "ใช้กฎข้อที่สองของนิวตัน a = Fสุทธิ/m = 12 N / 3 kg = 4 m/s²",
        "source_quote": "เมื่อทราบแรงลัพธ์และมวล สามารถคำนวณความเร่งจากกฎข้อที่สองของนิวตัน Fสุทธิ = ma ได้โดยตรง",
    },
    {
        "stem": "วัตถุสองชิ้นเริ่มจากหยุดนิ่งและถูกเร่งจนมีความเร็วปลายเท่ากัน วัตถุ A มีมวล 2 kg และวัตถุ B มีมวล 4 kg วัตถุใดต้องใช้แรงมากกว่าเพื่อให้ถึงความเร็วนั้น?",
        "choices": [
            "วัตถุ B เพราะมีมวลมากกว่า",
            "วัตถุ A เพราะมีมวลน้อยกว่า",
            "ทั้งสองใช้แรงเท่ากัน",
            "วัตถุที่เบากว่าต้องใช้แรงมากกว่า",
        ],
        "explanation": "ถ้าต้องเร่งวัตถุให้มีความเร็วเท่ากัน วัตถุที่มวลมากกว่าจะต้องใช้แรงมากกว่า ดังนั้นวัตถุ B ต้องใช้แรงมากกว่า",
        "source_quote": "การเร่งวัตถุที่มีมวลมากกว่าให้มีความเร็วเท่ากัน ย่อมต้องใช้แรงมากกว่า",
    },
    {
        "stem": "คนคนหนึ่งผลักรถเข็นด้วยแรงหนึ่ง ทำให้รถเข็นมีความเร่ง 2 m/s² หากออกแรงเท่าเดิมกับรถเข็นที่บรรทุกของจนมีมวลเป็นสองเท่า ความเร่งจะเปลี่ยนเป็นเท่าใด?",
        "choices": ["1 m/s²", "4 m/s²", "ยังคงเป็น 2 m/s²", "0.5 m/s²"],
        "explanation": "เมื่อใช้แรงเท่าเดิมกับวัตถุที่มีมวลมากขึ้นเป็นสองเท่า ความเร่งจะลดลงครึ่งหนึ่ง จาก 2 m/s² เหลือ 1 m/s²",
        "source_quote": "การผลักรถที่มีมวลมากกว่าด้วยแรงเท่าเดิมทำให้เกิดความเร่งน้อยกว่ามาก โดยไม่คิดแรงเสียดทาน",
    },
    {
        "stem": "คนคนหนึ่งยืนบนสเกตบอร์ดแล้วผลักกำแพง แรงใดเป็นแรงภายนอกของระบบคน-สเกตบอร์ดและทำให้คนเคลื่อนที่?",
        "choices": [
            "แรงที่กำแพงกระทำต่อคน",
            "แรงที่คนกระทำต่อกำแพง",
            "แรงที่คนกระทำต่อสเกตบอร์ด",
            "แรงที่สเกตบอร์ดกระทำต่อคน",
        ],
        "explanation": "แรงที่กำแพงกระทำต่อคนเป็นแรงภายนอกของระบบคน-สเกตบอร์ดและทำให้ระบบเกิดความเร่ง ส่วนแรงที่คนกระทำต่อกำแพงกระทำอยู่นอกระบบ",
        "source_quote": "กฎข้อที่สามของนิวตันช่วยระบุได้ว่าแรงใดเป็นแรงภายนอกของระบบ",
    },
    {
        "stem": "วัตถุมวล 2.0 kg ถูกปล่อยใกล้ผิวโลก โดยไม่คิดแรงต้านอากาศ ขนาดของแรงภายนอกลัพธ์ที่กระทำต่อวัตถุมีค่าเท่าใด?",
        "choices": ["19.6 N", "9.8 N", "2.0 N", "0 N"],
        "explanation": "แรงลัพธ์เท่ากับน้ำหนัก W = mg = 2.0 × 9.8 = 19.6 N",
        "source_quote": "ถ้าแรงต้านอากาศมีค่าน้อยมาก แรงภายนอกลัพธ์ของวัตถุที่ตกลงมาจะมีเฉพาะแรงโน้มถ่วง หรือน้ำหนัก",
    },
    {
        "stem": "มีแรงภายนอกลัพธ์ 12 N กระทำต่อวัตถุมวล 3.0 kg ขนาดความเร่งของวัตถุมีค่าเท่าใด?",
        "choices": ["4.0 m/s²", "0.25 m/s²", "36 m/s²", "9.0 m/s²"],
        "explanation": "จากกฎข้อที่สองของนิวตัน a = Fสุทธิ/m = 12 / 3.0 = 4.0 m/s²",
        "source_quote": "เมื่อทราบแรงลัพธ์และมวล สามารถคำนวณความเร่งจากกฎข้อที่สองของนิวตัน Fสุทธิ = ma ได้โดยตรง",
    },
    {
        "stem": "วัตถุสองชิ้นมวล 2 kg และ 8 kg เริ่มจากหยุดนิ่ง มีแรงลัพธ์เท่ากันกระทำเป็นเวลาเท่ากัน ข้อใดเปรียบเทียบแรงที่ต้องใช้เพื่อให้ทั้งสองมีความเร็วปลายเท่ากันได้ถูกต้อง?",
        "choices": [
            "วัตถุ 8 kg ต้องใช้แรงมากกว่าวัตถุ 2 kg เป็น 4 เท่า",
            "วัตถุ 2 kg ต้องใช้แรงมากกว่าวัตถุ 8 kg เป็น 4 เท่า",
            "ใช้แรงเท่ากัน เพราะทั้งสองเริ่มจากหยุดนิ่ง",
            "วัตถุ 8 kg ต้องใช้แรงมากกว่าวัตถุ 2 kg เป็น 2 เท่า",
        ],
        "explanation": "เพื่อให้ได้ความเร็วเท่ากันในเวลาเท่ากัน ทั้งสองต้องมีความเร่งเท่ากัน และจาก F = ma แรงแปรผันตามมวล วัตถุ 8 kg จึงต้องใช้แรงมากกว่า 4 เท่า",
        "source_quote": "การเร่งวัตถุที่มีมวลมากกว่าให้มีความเร็วเท่ากัน ย่อมต้องใช้แรงมากกว่า",
    },
    {
        "stem": "เด็กชายผลักลูกบาสเกตบอลด้วยแรง 20 N ทำให้เกิดความเร่ง 40 m/s² หากผลักรถยนต์ที่หยุดนิ่งด้วยแรงเท่าเดิม และรถยนต์มีมวลเป็น 1,000 เท่าของลูกบาสเกตบอล ความเร่งของรถยนต์จะเป็นเท่าใด? (ไม่คิดแรงเสียดทาน)",
        "choices": ["0.04 m/s²", "0.4 m/s²", "4 m/s²", "40 m/s²"],
        "explanation": "มวลลูกบาสเกตบอลคือ 20/40 = 0.5 kg รถยนต์จึงมีมวล 500 kg และความเร่งคือ 20/500 = 0.04 m/s²",
        "source_quote": "การผลักรถที่มีมวลมากกว่าด้วยแรงเท่าเดิมทำให้เกิดความเร่งน้อยกว่ามาก โดยไม่คิดแรงเสียดทาน",
    },
    {
        "stem": "คนคนหนึ่งยืนบนสเกตบอร์ดแล้วผลักกำแพง กำแพงกระทำแรงต่อคน แรงใดควรรวมอยู่ในแรงภายนอกลัพธ์ของคนเพื่อหาความเร่งของเขา?",
        "choices": [
            "แรงที่กำแพงกระทำต่อคน",
            "แรงที่คนกระทำต่อกำแพง",
            "ทั้งแรงที่กระทำต่อกำแพงและแรงที่กระทำต่อคน",
            "ไม่ต้องรวมแรงใด เพราะแรงทั้งสองหักล้างกัน",
        ],
        "explanation": "ในการหาแรงลัพธ์ของคน ต้องรวมเฉพาะแรงที่กระทำต่อคน แรงที่คนกระทำต่อกำแพงกระทำต่อวัตถุอื่นจึงไม่รวม",
        "source_quote": "กฎข้อที่สามของนิวตันช่วยระบุได้ว่าแรงใดเป็นแรงภายนอกของระบบ",
    },
    {
        "stem": "วัตถุมวล 3.0 kg ถูกปล่อยใกล้ผิวโลกโดยไม่คิดแรงต้านอากาศ ใช้ W = mg และ g = 9.8 m/s² น้ำหนักของวัตถุมีค่าเท่าใดในหน่วยนิวตัน?",
        "choices": ["29.4 N", "3.0 N", "9.8 N", "32.8 N"],
        "explanation": "น้ำหนักเป็นแรงโน้มถ่วงบนมวล ใช้ W = mg = 3.0 × 9.8 = 29.4 N",
        "source_quote": "น้ำหนักคือแรงโน้มถ่วงที่กระทำต่อมวล m และคำนวณได้จาก W = mg",
    },
    {
        "stem": "วัตถุมวล 2.0 kg ถูกนำไปไว้บนดวงจันทร์ ซึ่งมีความเร่งเนื่องจากแรงโน้มถ่วง 1.67 m/s² น้ำหนักของวัตถุบนดวงจันทร์มีค่าเท่าใด?",
        "choices": ["3.34 N", "1.67 N", "2.0 N", "19.6 N"],
        "explanation": "ใช้ W = mg = 2.0 kg × 1.67 m/s² = 3.34 N",
        "source_quote": "น้ำหนักขึ้นกับแรงโน้มถ่วง มวล 1.0 kg หนัก 9.8 N บนโลก แต่หนักประมาณ 1.7 N บนดวงจันทร์",
    },
    {
        "stem": "ถ้า 1 N = 0.225 lb แรง 5 N มีค่าเท่าใดเมื่อแปลงเป็นปอนด์?",
        "choices": ["1.125 lb", "0.045 lb", "2.025 lb", "5.000 lb"],
        "explanation": "คูณแรงในหน่วยนิวตันด้วยตัวคูณแปลงหน่วย: 5 × 0.225 = 1.125 lb",
        "source_quote": "ถ้าแรง 1 N เท่ากับ 0.225 lb แรง 5 N จะมีค่าเป็นกี่ปอนด์",
    },
    {
        "stem": "มีแรง 8 N กระทำต่อวัตถุ แรงนี้มีค่าเท่าใดในหน่วยปอนด์ เมื่อกำหนดให้ 1 N = 0.225 lb?",
        "choices": ["1.8 lb", "0.028 lb", "3.6 lb", "8.0 lb"],
        "explanation": "ใช้ตัวคูณแปลงหน่วย 0.225 lb/N: 8 × 0.225 = 1.8 lb",
        "source_quote": "แม้คนส่วนใหญ่ใช้หน่วยนิวตัน แต่หน่วยแรงที่คุ้นเคยในสหรัฐอเมริกาคือปอนด์ โดย 1 N = 0.225 lb",
    },
    {
        "stem": "มีแรงภายนอกลัพธ์ 51 N กระทำต่อเครื่องตัดหญ้ามวล 240 kg ขนาดความเร่งของเครื่องตัดหญ้ามีค่าเท่าใด?",
        "choices": ["0.21 m/s²", "0.51 m/s²", "4.7 m/s²", "12,240 m/s²"],
        "explanation": "จาก Fสุทธิ = ma ได้ a = Fสุทธิ/m = 51/240 = 0.2125 m/s² หรือประมาณ 0.21 m/s²",
        "source_quote": "เมื่อทราบแรงลัพธ์และมวล สามารถคำนวณความเร่งจากกฎข้อที่สองของนิวตัน Fสุทธิ = ma ได้โดยตรง",
    },
]


def translate_gate_reason(reason: str) -> str:
    if reason == "structurally sound":
        return "โครงสร้างถูกต้อง"
    if reason == "no arithmetic in this question":
        return "ข้อนี้ไม่มีการคำนวณเลข"
    if reason == "dimensionless or no physical unit declared":
        return "ไม่มีหน่วยกายภาพที่ต้องตรวจ"
    if reason == "distinct from the questions already accepted":
        return "ไม่ซ้ำกับข้อที่ผ่านมาก่อนหน้า"
    if reason.startswith("verbatim in chunk"):
        return "ตรงกับข้อความต้นฉบับใน " + reason.removeprefix("verbatim in ")
    if reason.startswith("chunk "):
        return "ใช้ข้อความจาก " + reason
    if reason.startswith("slot "):
        return "ตรงตามช่องความครอบคลุม: " + reason
    if ", matches the correct choice" in reason:
        return reason.replace(", matches the correct choice", " และตรงกับตัวเลือกที่ถูกต้อง")
    return reason


def normalize_gates(raw_gates: list[dict]) -> list[dict]:
    return [
        {
            "gate": GATE_TH.get(gate.get("Gate", ""), gate.get("Gate", "")),
            "pass": bool(gate.get("Pass")),
            "reason": translate_gate_reason(gate.get("Reason", "")),
        }
        for gate in raw_gates
    ]


def load_ours(path: Path) -> list[dict]:
    report = json.loads(path.read_text(encoding="utf-8"))
    rows: list[dict] = []
    for case in report.get("cases", []):
        case_name = case.get("name", "run")
        for index, question in enumerate(case.get("questions", []), start=1):
            thai = OURS_THAI[len(rows)] if len(rows) < len(OURS_THAI) else {}
            rows.append(
                {
                    "source": "ours",
                    "source_label": "ของเรา · ชุดฟิสิกส์ล่าสุด",
                    "group": f"ours:{case_name}",
                    "group_label": CASE_LABELS.get(case_name, case_name),
                    "index": index,
                    "stem": thai.get("stem", question.get("stem", "")),
                    "choices": thai.get(
                        "choices",
                        [c.get("content", "") for c in question.get("choices", [])],
                    ),
                    "correct": next(
                        (i for i, c in enumerate(question.get("choices", [])) if c.get("is_correct")),
                        None,
                    ),
                    "passed": bool(question.get("passed")),
                    "skill": SKILL_TH.get(question.get("skill", ""), question.get("skill", "")),
                    "difficulty": DIFFICULTY_TH.get(
                        question.get("difficulty", ""), question.get("difficulty", "")
                    ),
                    "explanation": thai.get("explanation", question.get("explanation", "")),
                    "source_quote": thai.get("source_quote", question.get("source_quote", "")),
                    "coverage_slot_id": question.get("coverage_slot_id", ""),
                    "evidence_atom_id": question.get("evidence_atom_id", ""),
                    "evidence_chunk_id": question.get("evidence_chunk_id", ""),
                    "gates": normalize_gates(question.get("gates", [])),
                }
            )
    return rows


def load_nlm(path: Path, source: str, label: str, detail: str) -> list[dict]:
    rows: list[dict] = []
    for index, question in enumerate(parse_notebooklm(path), start=1):
        category = CATEGORIES[min((index - 1) // 5, len(CATEGORIES) - 1)]
        rows.append(
            {
                "source": source,
                "source_label": label,
                "group": f"{source}:{category}",
                "group_label": f"{label} · {category}",
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
                "detail": detail,
            }
        )
    return rows


def js_json(value: object) -> str:
    return json.dumps(value, ensure_ascii=False).replace("</", "<\\/")


def build_html(ours: list[dict], sources: list[dict], metadata: dict) -> str:
    data = ours + [row for source in sources for row in source["rows"]]
    source_meta = [
        {"key": source["key"], "label": source["label"], "detail": source["detail"]}
        for source in sources
    ]
    all_sources = [
        {"key": "ours", "label": "ของเรา · ชุดฟิสิกส์ล่าสุด", "detail": "ชุดที่สร้างล่าสุด"}
    ]
    all_sources.extend(source_meta)
    metadata = {**metadata, "sources": all_sources}

    css_parts = []
    for key, (border, background, text) in SOURCE_STYLES.items():
        css_parts.append(
            f".card.{key} {{ border-left-color:{border}; }}"
            f".badge.{key} {{ background:{background}; color:{text}; }}"
        )
    source_buttons = "".join(
        f'<button data-filter="{html.escape(source["key"], quote=True)}">'
        f'{html.escape(source["label"])}</button>'
        for source in all_sources
    )
    warning = (
        "แหล่งข้อมูลหลักเป็น OpenStax Physics เดียวกันทั้งหมด ฝั่งของเราและ "
        "NotebookLM · ทั้งแหล่งข้อมูลใช้ชุดฟิสิกส์จากทั้งเอกสาร ส่วน NotebookLM · "
        "บางส่วนจำกัดไว้ที่การเคลื่อนที่แนวตรง กฎของนิวตัน งานและพลังงาน โมเมนตัม "
        "และกฎของโอห์ม หน้านี้ใช้ดูเนื้อหาและหลักฐาน ไม่ตัดสินคะแนนอัตโนมัติ"
    )

    template = r"""<!doctype html>
<html lang="th">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>เปรียบเทียบข้อสอบฟิสิกส์ · ของเรา vs NotebookLM</title>
<style>
:root { color-scheme: light; --ink:#172033; --muted:#64748b; --line:#dbe2ea; --panel:#fff; --bg:#f4f7fb; --good:#087443; --bad:#b42318; }
* { box-sizing:border-box; }
body { margin:0; background:var(--bg); color:var(--ink); font:15px/1.65 system-ui,-apple-system,"Segoe UI","Noto Sans Thai",sans-serif; }
header { padding:28px max(20px,calc((100vw - 1500px)/2)); background:#101828; color:#fff; }
h1 { margin:0 0 8px; font-size:25px; }
h2 { margin:0; font-size:19px; }
p { margin:7px 0; }
.subtle { color:#b8c2d1; overflow-wrap:anywhere; }
.warning { max-width:1500px; margin:18px auto 0; padding:14px 18px; border:1px solid #f3c55b; border-radius:12px; background:#fff8df; color:#694b00; }
.warning strong { display:block; margin-bottom:4px; }
.toolbar { position:sticky; top:0; z-index:4; padding:12px max(20px,calc((100vw - 1500px)/2)); background:rgba(244,247,251,.95); border-bottom:1px solid var(--line); backdrop-filter:blur(8px); }
.toolbar-inner { display:flex; flex-wrap:wrap; gap:8px; align-items:center; }
button, input { font:inherit; }
button { border:1px solid var(--line); border-radius:9px; padding:8px 12px; background:#fff; color:var(--ink); cursor:pointer; }
button.active { background:#172033; color:#fff; border-color:#172033; }
input { min-width:260px; flex:1; border:1px solid var(--line); border-radius:9px; padding:9px 12px; background:#fff; }
main { max-width:1500px; margin:0 auto; padding:20px; }
.summary { display:grid; grid-template-columns:repeat(auto-fit,minmax(190px,1fr)); gap:10px; margin-bottom:18px; }
.metric { padding:13px 15px; background:var(--panel); border:1px solid var(--line); border-radius:12px; }
.metric b { display:block; font-size:21px; }
.metric span { color:var(--muted); font-size:13px; }
.section { margin:22px 0 30px; }
.section-title { display:flex; gap:10px; align-items:baseline; margin-bottom:10px; }
.section-title small { color:var(--muted); }
.grid { display:grid; grid-template-columns:repeat(auto-fit,minmax(330px,1fr)); gap:13px; }
.card { background:var(--panel); border:1px solid var(--line); border-left:5px solid var(--line); border-radius:12px; padding:15px; box-shadow:0 2px 7px #1720330b; }
.card-head { display:flex; justify-content:space-between; gap:8px; align-items:start; margin-bottom:9px; }
.badge { display:inline-block; padding:3px 8px; border-radius:999px; background:#eef2f7; color:var(--muted); font-size:12px; }
.stem { font-weight:650; margin-bottom:10px; }
.choices { display:grid; gap:5px; margin:0; padding:0; list-style:none; }
.choice { padding:7px 9px; border:1px solid #e5eaf0; border-radius:8px; }
.choice.correct { border-color:#8bd5ad; background:#effcf4; }
.letter { display:inline-block; width:24px; color:var(--muted); font-weight:700; }
.details { margin-top:12px; border-top:1px solid #eef1f5; padding-top:8px; }
summary { cursor:pointer; color:#344054; font-weight:600; }
.detail-grid { display:grid; grid-template-columns:150px 1fr; gap:5px 10px; margin-top:9px; font-size:13px; }
.detail-grid dt { color:var(--muted); } .detail-grid dd { margin:0; overflow-wrap:anywhere; }
code { font-family:ui-monospace,SFMono-Regular,Consolas,monospace; font-size:12px; }
.gate-fail { color:var(--bad); } .gate-pass { color:var(--good); }
.empty { padding:30px; color:var(--muted); text-align:center; background:#fff; border:1px dashed var(--line); border-radius:12px; }
.matrix { overflow:auto; }
table { width:100%; min-width:1050px; border-collapse:separate; border-spacing:0 8px; }
th { text-align:left; color:var(--muted); font-size:12px; padding:0 10px 3px; }
td { vertical-align:top; width:var(--column-width); padding:12px; background:#fff; border-top:1px solid var(--line); border-bottom:1px solid var(--line); }
td:first-child { border-left:1px solid var(--line); border-radius:10px 0 0 10px; } td:last-child { border-right:1px solid var(--line); border-radius:0 10px 10px 0; }
.matrix .stem { font-size:14px; }
@media(max-width:650px) { .detail-grid { grid-template-columns:1fr; gap:1px; } input { min-width:180px; } }
__SOURCE_CSS__
</style>
</head>
<body>
<header>
  <h1>เปรียบเทียบข้อสอบฟิสิกส์ · ของเรา vs NotebookLM</h1>
  <p class="subtle" id="meta"></p>
  <div class="warning"><strong>วิธีอ่านหน้านี้</strong>__WARNING__</div>
</header>
<div class="toolbar"><div class="toolbar-inner">
  <button class="active" data-filter="matrix">มุมมองจับคู่</button>
  __SOURCE_BUTTONS__
  <button data-filter="all">การ์ดทั้งหมด</button>
  <input id="search" placeholder="ค้นหาโจทย์ ตัวเลือก หลักฐาน หรือ ID…">
</div></div>
<main>
  <div class="summary" id="summary"></div>
  <section id="content"></section>
</main>
<script>
const DATA = __DATA_JSON__;
const META = __META_JSON__;
const SOURCES = META.sources;
const letters = ["A", "B", "C", "D"];
const esc = value => String(value == null ? "" : value).replace(/[&<>"']/g, function(ch) {
  return {"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[ch];
});
document.getElementById("meta").textContent = "ไฟล์ข้อมูล: " + META.paths.join(" · ") + " · ชุด: " + META.suite;

function card(q) {
  const choices = (q.choices || []).map(function(choice, i) {
    return '<li class="choice ' + (q.correct === i ? "correct" : "") + '"><span class="letter">' +
      (letters[i] || "?") + '.</span>' + esc(choice) + '</li>';
  }).join("");
  const gates = (q.gates || []).map(function(g) {
    return '<div class="' + (g.pass ? "gate-pass" : "gate-fail") + '">' +
      (g.pass ? "✓" : "✗") + " " + esc(g.gate) + " — " + esc(g.reason) + "</div>";
  }).join("");
  const status = q.source === "ours" ? '<span class="badge ' + q.source + '">' +
    (q.passed ? "ผ่าน gate" : "ไม่ผ่าน gate") + "</span>" : "";
  const details = q.source === "ours" ? '<dl class="detail-grid">' +
    "<dt>ทักษะ / ระดับ</dt><dd>" + esc(q.skill) + " / " + esc(q.difficulty) + "</dd>" +
    "<dt>ช่องความครอบคลุม</dt><dd><code>" + esc(q.coverage_slot_id) + "</code></dd>" +
    "<dt>หน่วยหลักฐาน</dt><dd><code>" + esc(q.evidence_atom_id) + "</code></dd>" +
    "<dt>ช่วงหลักฐาน</dt><dd><code>" + esc(q.evidence_chunk_id) + "</code></dd>" +
    "<dt>ข้อความอ้างอิง</dt><dd>" + esc(q.source_quote) + "</dd>" +
    "<dt>คำอธิบาย</dt><dd>" + esc(q.explanation) + "</dd></dl>" +
    '<div class="details-gates">' + gates + "</div>" :
    '<dl class="detail-grid"><dt>คำตอบที่ระบุ</dt><dd>' +
    (q.correct == null ? "ไม่ได้ระบุ" : letters[q.correct]) + "</dd></dl>";
  return '<article class="card ' + q.source + '"><div class="card-head"><div><span class="badge ' +
    q.source + '">' + esc(q.source_label) + '</span><div><small>ข้อที่ ' + q.index + " · " +
    esc(q.group_label) + "</small></div></div>" + status + '</div><div class="stem">' +
    esc(q.stem) + '</div><ul class="choices">' + choices + '</ul><details class="details">' +
    "<summary>หลักฐาน / รายละเอียด QC</summary>" + details + "</details></article>";
}

function matches(q, query) {
  if (!query) return true;
  return JSON.stringify(q).toLowerCase().includes(query.toLowerCase());
}

function summary() {
  const metrics = SOURCES.map(function(source) {
    const rows = DATA.filter(function(q) { return q.source === source.key; });
    const value = source.key === "ours" ?
      rows.filter(function(q) { return q.passed; }).length + "/" + rows.length + " ผ่าน gate" :
      rows.length + " ข้อ";
    return [source.label, value, source.detail];
  });
  metrics.push(["โหมดเปรียบเทียบ", "ดูด้วยตา", "แหล่งข้อมูล OpenStax Physics เดียวกัน"]);
  document.getElementById("summary").innerHTML = metrics.map(function(item) {
    return '<div class="metric"><span>' + esc(item[0]) + "</span><b>" +
      esc(item[1]) + '</b><span>' + esc(item[2]) + "</span></div>";
  }).join("");
}

function matrix(query) {
  const columns = SOURCES.map(function(source) {
    return DATA.filter(function(q) { return q.source === source.key; });
  });
  const max = Math.max.apply(null, columns.map(function(column) { return column.length; }));
  let rows = "";
  for (let i = 0; i < max; i++) {
    const cells = columns.map(function(column) {
      const q = column[i];
      return q && matches(q, query) ? "<td>" + card(q) + "</td>" : "<td></td>";
    }).join("");
    if (cells.replace(/<td><\/td>/g, "").trim()) rows += "<tr>" + cells + "</tr>";
  }
  const headings = SOURCES.map(function(source) { return "<th>" + esc(source.label) + "</th>"; }).join("");
  return '<div class="matrix"><table><thead><tr>' + headings +
    "</tr></thead><tbody>" + rows + "</tbody></table></div>";
}

function cards(source, query) {
  const groups = [...new Set(DATA.filter(function(q) {
    return source === "all" || q.source === source;
  }).map(function(q) { return q.group; }))];
  return groups.map(function(group) {
    const questions = DATA.filter(function(q) {
      return q.group === group && (source === "all" || q.source === source) && matches(q, query);
    });
    if (!questions.length) return "";
    return '<section class="section"><div class="section-title"><h2>' +
      esc(questions[0].group_label) + "</h2><small>" + questions.length +
      ' ข้อที่แสดง</small></div><div class="grid">' + questions.map(card).join("") +
      "</div></section>";
  }).join("") || '<div class="empty">ไม่พบข้อที่ตรงกับคำค้น</div>';
}

function render() {
  const filter = document.querySelector("button.active").dataset.filter;
  const query = document.getElementById("search").value.trim();
  document.getElementById("content").innerHTML = filter === "matrix" ? matrix(query) : cards(filter, query);
}
document.querySelectorAll("button[data-filter]").forEach(function(button) {
  button.addEventListener("click", function() {
    document.querySelectorAll("button[data-filter]").forEach(function(b) { b.classList.remove("active"); });
    button.classList.add("active");
    render();
  });
});
document.getElementById("search").addEventListener("input", render);
summary();
render();
</script>
</body>
</html>
"""
    return (
        template.replace("__SOURCE_CSS__", "\n".join(css_parts))
        .replace("__WARNING__", warning)
        .replace("__SOURCE_BUTTONS__", source_buttons)
        .replace("__DATA_JSON__", js_json(data))
        .replace("__META_JSON__", js_json(metadata))
    )


def append_source(
    sources: list[dict],
    path: Path,
    key: str,
    label: str,
    detail: str,
) -> None:
    sources.append(
        {
            "key": key,
            "label": label,
            "detail": detail,
            "path": path,
            "rows": load_nlm(path, key, label, detail),
        }
    )


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--ours", type=Path, required=True, help="benchmark JSON")
    parser.add_argument("--nlm-whole", "--nlm-thai", dest="nlm_whole", type=Path)
    parser.add_argument("--nlm-partial", type=Path)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()

    sources: list[dict] = []
    if args.nlm_whole:
        append_source(
            sources,
            args.nlm_whole,
            "nlm-whole",
            "NotebookLM · ทั้งแหล่งข้อมูล",
            "15 ข้อจากทั้งเอกสาร · ภาษาไทย",
        )
    if args.nlm_partial:
        append_source(
            sources,
            args.nlm_partial,
            "nlm-partial",
            "NotebookLM · บางส่วน",
            "15 ข้อจากหัวข้อที่จำกัด · ภาษาไทย",
        )
    if not sources:
        parser.error("ต้องระบุไฟล์ผลลัพธ์จาก NotebookLM อย่างน้อยหนึ่งไฟล์")

    ours = load_ours(args.ours)
    report = json.loads(args.ours.read_text(encoding="utf-8"))
    metadata = {
        "paths": [str(args.ours)] + [str(source["path"]) for source in sources],
        "suite": report.get("suite", ""),
    }
    args.out.parent.mkdir(parents=True, exist_ok=True)
    args.out.write_text(build_html(ours, sources, metadata), encoding="utf-8")
    counts = " + ".join(f'{len(source["rows"])} {source["label"]}' for source in sources)
    print(f"wrote {args.out} ({len(ours)} ของเรา + {counts})")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
