# Exam generation quality research — 2026-08-06

เอกสารนี้สรุป research ที่ใช้ตัดสินใจรอบ cross-subject benchmark และการปรับ
prototype ให้ได้คุณภาพต่อ token สูงขึ้น ตั้งแต่ extraction ถึง acceptance

## หลักคิดที่ยึด

### 1. เริ่มจาก assessment contract ไม่ใช่ความมั่นใจของโมเดล

ข้อสอบที่ดีต้องผูกเป็นสายเดียวกัน:

```text
construct → evidence → task → keyed answer → distractor rationale → source span
```

ดังนั้น “model ตอบได้” ไม่ใช่เกณฑ์พอ เพราะโมเดลอาจตอบจากความรู้เดิมหรือเดาคำตอบ
ได้โดยไม่ใช้ย่อหน้าที่กำหนด เกณฑ์ runtime จึงตรวจ evidence/contract ก่อน ส่วน
semantic quality ตรวจความดีทางการสอนแยกต่างหาก

แหล่งหลัก: [ETS Evidence-Centered Design primer](https://files.eric.ed.gov/fulltext/ED483399.pdf),
[National Academies assessment triangle](https://www.nationalacademies.org/read/10019/chapter/2)
และ [AERA/APA/NCME Standards](https://www.testingstandards.net/uploads/7/6/6/4/76643089/standards_2014edition.pdf)

### 2. ยากต้องยากที่ reasoning ไม่ใช่ถ้อยคำกวน

ใช้สามระดับเป็น contract ไม่ใช่แค่ label:

- **ง่าย**: ดึงข้อเท็จจริง/ความสัมพันธ์ตรง ๆ หรือทำ operation เดียว
- **กลาง**: ใช้ความสัมพันธ์กับเงื่อนไขใหม่ มีการแยกแยะหรืออนุมานอย่างน้อยหนึ่งครั้ง
- **ยาก**: มีอย่างน้อยสอง linked steps หรือ competing constraints และแต่ละขั้น
  จำเป็นต่อคำตอบ; ข้อมูลลวงต้องเป็นข้อมูลที่เกี่ยวข้อง ไม่ใช่ความกำกวมที่ไม่ยุติธรรม

กรอบนี้สอดคล้องกับ cognitive-demand ของ [TIMSS](https://timssandpirls.bc.edu/timss-2019/frameworks/frameworks.html)
และ [PISA](https://www.oecd.org/en/about/programmes/pisa/pisa-test.html) และหลัก
one-best-answer/distractor ที่ใช้งานจริงใน [NBME item-writing guide](https://info.nbme.org/rs/552-QHC-046/images/NBME_Item-Writing-Guide.pdf?version=0)
กับแนวทาง MCQ ของ [Haladyna et al.](https://experts.umn.edu/en/publications/a-review-of-multiple-choice-item-writing-guidelines-for-classroom/)

ข้อสรุปเชิง implementation: `difficulty` ต้องมีหลักฐานประกอบเป็น
`changed_condition`, `reasoning_steps`, `operation` และ `distractor_reasons`;
ห้ามรับคำอธิบายที่เติมคำว่า “ดังนั้น/เพราะว่า” ให้ดูเหมือนหลายขั้น

### 3. Graph เป็น routing/index; raw chunk เป็นหลักฐาน

งาน RAG และ long-context ชี้ตรงกันว่า context ที่ยาวขึ้นไม่ได้แปลว่า reasoning
ดีขึ้น และข้อมูลสำคัญอาจหลุดเมื่ออยู่กลาง context. จึงใช้ graph เพื่อเลือก atom,
relation และ neighbor แต่ส่ง quote/chunk ดิบที่ตรวจย้อนกลับได้ให้ writer

แหล่งอ้างอิง: [RAG](https://arxiv.org/abs/2005.11401),
[Lost in the Middle](https://arxiv.org/abs/2307.03172),
[RAPTOR](https://arxiv.org/abs/2401.18059) และ
[GraphRAG query overview](https://microsoft.github.io/graphrag/query/overview/)

ลำดับที่คุ้ม token ที่สุดจึงเป็น:

1. เลือก slot/atom ก่อน document order
2. ส่ง atom หลัก + support atom ที่จำเป็น
3. ส่ง raw chunk ของ slot เพื่อ exact quote
4. เพิ่ม typed neighbor เพียงเล็กน้อยเมื่อ relation ต้องข้าม chunk
5. cap packet ตาม rune/token budget

### 4. Extraction ยังเป็นคอขวดที่ต้องวัด ไม่ใช่โยนให้ graph แก้

prototype ปัจจุบันมี text/page chunking และ graph normalization ที่ตรวจ quote กับ
chunk ได้ แต่ยังไม่ได้ใช้ layout/table/image asset เป็น first-class evidence ทั้งหมด.
งานต่อไปจึงควรมี extraction diagnostics อย่างน้อย: text density ต่อหน้า, heading
boundary, table loss, empty/scanned page และ quote coverage ก่อนเริ่ม graph compile.

[Docling](https://github.com/docling-project/docling) และแนวคิด
[chunking](https://docling-project.github.io/docling/concepts/chunking/) สนับสนุน
การเก็บโครงสร้างเอกสาร/metadata ไว้กับ chunk แทนการตัดเป็นข้อความแบนอย่างเดียว.
แต่การเพิ่ม layout pipeline มี cost และความเสี่ยงมากกว่า slot-local packet จึงยัง
ไม่ promote ในรอบนี้

### 5. Semantic grader เป็น advisory

LLM judge มี bias และความแปรปรวน; งาน [G-Eval](https://aclanthology.org/2023.emnlp-main.153/)
เองก็เป็นหลักฐานว่าการประเมินแบบนี้ต้อง calibrate ไม่ใช่ใช้เป็น truth. ในระบบนี้
จึงแยกผลดังนี้:

- deterministic gate: กันข้อ application medium/hard ที่ไม่มี demand evidence และ
  กันข้อที่ผิด contract หรือ verify ไม่ได้
- semantic grader: ให้คะแนน groundedness/correctness/distractor/difficulty-fit
  เพื่อเลือก candidate หรือช่วย audit
- human sample: ใช้ตรวจว่า grader drift หรือ generator self-review หลอกตัวเองหรือไม่

## Dynamic workflow และ cost policy

```text
PDF/document
  → extract + diagnostics
  → keep content chunks only for cold graph pass
  → graph/outline compile (DeepSeek 4 chunks / 8k default)
  → build coverage slots by skill × difficulty
  → preflight and drop unsupported slots
  → construct slot-local packet
  → one set-generation call
  → deterministic gates
  → bounded repair only for failed/missing slots
  → report raw drafts, ship-ready items, semantic review, token/call cost
```

กติกาเลือกการเพิ่มชั้น:

- ถ้าเพิ่ม quality signal โดยไม่เพิ่ม LLM call: ทำก่อน
- ถ้าเพิ่ม call: ต้องพิสูจน์ว่า accepted semantic quality ต่อ tokenดีขึ้นจาก
  baseline เดิม ไม่ใช่แค่ gate pass สูงขึ้น
- ถ้า failure เป็น provenance/contract: แก้ prompt/schema/gate
- ถ้า failure เป็น extraction: แก้ chunk/asset diagnostics ก่อนเพิ่ม judge
- ถ้า failure เป็น genuine semantic difficulty: ค่อยใช้ independent reviewer หรือ
  human calibration sample

## สิ่งที่ benchmark รอบนี้ยืนยัน

hard/application targeted rerun หลังเพิ่ม support atoms:

| subject | ship-ready / target | raw drafts | failure signal |
|---|---:|---:|---|
| math | 3/3 | 3 | ผ่าน linked reasoning |
| physics | 3/3 | 3 | ผ่าน changed condition + multi-step |
| biology | 2/2 | 2 | ผ่าน causal/conditional application |
| psychology | 3/3 | 3 | ผ่าน source-specific application |
| sociology | 3/3 | 4 | invisible passage reference remains as the only failed draft |
| economics | 3/3 | 5 | non-verbatim quote; missing operation |

นี่เป็นหลักฐานว่า contract ช่วยกัน recall ที่ติดป้าย hard ได้ แต่ยังไม่ใช่
หลักฐานว่า semantic quality ถึง NotebookLM แล้ว. ต้องอ่านตัวอย่างที่ผ่านและ
failure rows; ห้ามสรุปจาก gate summary อย่างเดียว

## สิ่งที่ยังต้องทำต่อ

1. เพิ่ม extraction diagnostics และรักษา table/figure/heading provenance ให้ครบ
2. ลด draft waste ด้วย prompt ที่ห้าม “เติมข้อเกิน” และ repair ที่ขอเฉพาะ slot ที่
   ตกจาก well-formed/operation/quote
3. สร้าง semantic benchmark rubric แบบ source-independent และ human-calibrate
   ตัวอย่างเล็ก ๆ ก่อนใช้เป็นตัวเลข release
4. เทียบ NotebookLM ด้วย source เดียวกัน, difficulty distribution เดียวกัน และ
   รายงานทั้ง raw drafts, ship-ready, semantic score, latency และ tokens
