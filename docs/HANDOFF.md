# Handoff — exam-quality prototype

อัปเดต 2026-08-06 บน branch `prototype/exam-quality` — สอง session ต่อเนื่องกันวันเดียว

session แรกจบที่ commit `0d7985c` (`refactor: make calculation an orthogonal
flag`) session ที่สอง (เอกสารนี้ครอบคลุมด้วย) แก้บั๊ก 4 ตัวใน calc/coverage
gate และเพิ่ม skill tier ใหม่ `analysis` — **โค้ดของ session ที่สองยังไม่ commit**
ดู `git status`/`git diff` ใน `backend/prototype-exam-quality/` ก่อนทำงานต่อ
ไฟล์ที่แก้ (9 ไฟล์): `app/benchmark.go` (+`benchmark_test.go`),
`examgen/evidence/evidence.go` (+`evidence_test.go`), `examgen/gates/gate.go`
(+`gate_test.go`), `examgen/generation/prompt.go` (ไม่มี test ไฟล์คู่ — แก้แค่
prompt/schema text), `examgen/model/calc.go` (+`calc_test.go`)

เอกสารนี้เป็นสถานะปัจจุบันสำหรับทำงานต่อ ผลทดลองเก่าที่ไม่ใช่ runtime path
ปัจจุบันถูกเอาออกจาก handoff แล้ว; รายละเอียดเชิงประวัติยังอยู่ใน artifact และ
`backend/prototype-exam-quality/VERDICT.md`.

## สถานะสั้น ๆ

prototype รันครบตั้งแต่ PDF ถึงข้อสอบ และมี provenance/contract QC ที่แน่นขึ้น
แล้ว แต่ยังไม่มีหลักฐานพอจะสรุปว่า “ชนะ NotebookLM” ใน semantic quality
เพราะการอ่านความยากจริงและคุณภาพตัวลวงยังต้องมี reviewer อิสระหรือ human sample
บน source เดียวกัน

```text
PDF
  → extraction diagnostics + content chunks
  → graph/outline compile
  → lesson + coverage contract
  → contract preflight
  → slot-local evidence packet
  → set generation
  → deterministic QC
  → optional advisory semantic/set review
  → report: drafts / ship-ready / review
```

## สิ่งที่ runtime ใช้อยู่ตอนนี้

- Graph compile ส่งเฉพาะ content chunks ในโหมดปกติ; มีโหมด `KeepAllTopics` สำหรับ
  regression/audit และมี fallback เมื่อผล compile ใช้ไม่ได้
- ก่อน generate จะเลือก evidence แบบ slot-local: atom หลัก, quote/chunk ที่ atom
  อ้างถึง และ neighbor ที่จำเป็น ไม่ส่ง evidence pool ทั้งบทเข้า writer
- Context จัดอันดับตาม slot/atom ก่อน document order และจำกัด context ที่ส่งให้
  writer เพื่อกัน evidence drift
- Contract preflight ซ่อมได้เฉพาะ defect ที่ deterministic เช่น atom/quote/chunk
  ไม่ตรงกัน; ไม่สร้าง evidence ใหม่แทนโมเดล
- Application/medium/hard ต้องมีสัญญาณการเปลี่ยนเงื่อนไขหรือ linked operation;
  hard ต้องมี support evidence และ reasoning อย่างน้อยสองขั้นเมื่อ source รองรับ
- `Operation` ต้องตรงกับ slot และข้อจะต้องอ้าง evidence ที่อยู่ใน packet ของมัน
- Calculator ถูกเรียกตาม slot ที่ต้องคำนวณ ไม่รวมทุก calculation/application chunk
  เป็นก้อนเดียว
- มี bounded retry เฉพาะ missing/failed slots; ไม่มี per-question judge call ใหม่

## Contract ปัจจุบัน

`calculation` ไม่ใช่ค่าใน `skill` เพราะเป็นวิธีดำเนินการ ไม่ใช่ cognitive demand
ของข้อสอบ:

```text
skill: recall | understanding | application | analysis
difficulty: easy | medium | hard
requires_calculation: true | false
calculation: { expression, expected, unit }  # ต้องมีเมื่อ flag เป็น true
```

ดังนั้น `application + hard + requires_calculation=true` เป็นรูปแบบที่ถูกต้อง
และ `understanding + easy + requires_calculation=true` ก็ถูกต้องเช่นกัน
`--force-calc` บังคับเฉพาะ flag ไม่ได้บังคับ skill หรือ difficulty

JSON เก่าที่มี `skill: calculation` ยังอ่านได้เป็น compatibility alias แล้วถูก
canonicalize เป็น skill ปกติพร้อม `requires_calculation=true`; ไม่ควรสร้าง artifact
ใหม่ด้วยรูปแบบเก่านี้

### `analysis` (skill ใหม่, session 2026-08-06)

เพิ่มเพื่อแก้ปัญหาที่ `application + hard` เดิมผ่านได้ด้วยสูตรเดียวใช้ซ้ำสองครั้ง
ไม่เคยบังคับว่าต้องรวมความคิดสองก้อนจริง `analysis` บังคับว่า supporting atom
ต้องมาจาก **chunk คนละก้อน** และ **relation type คนละแบบ** กับ atom หลัก
(`analysisSupportAtomIDs` ใน `examgen/evidence/evidence.go`)

**สำคัญ**: `analysis` **ไม่ผูกกับ difficulty=hard เสมอไป** — รอบแรกของ session
นี้ hardcode ไว้ว่า analysis ต้อง hard เท่านั้น (ทั้ง code และ system prompt เขียนว่า
"Analysis is always hard") ผู้ใช้ทักท้วงว่าไม่มีเหตุผลรองรับ Bloom's taxonomy
ถือว่า "analyze" เป็น*ประเภท*การคิด ไม่ใช่ระดับความยาก แก้แล้ว:

- `easy`/`medium` ต้องการ `reasoning_steps` อย่างน้อย 2 ขั้น (แค่รวม 2 ข้อเท็จจริง
  ที่เชื่อมกันตรงไปตรงมา)
- `hard` ต้องการอย่างน้อย 3 ขั้น (ต้องประนีประนอมข้อมูลที่ขัดแย้งหรือหลายขั้นจริง)
- ใน `buildCoverageContract` slot ของ analysis **ไม่ pin difficulty เลย**
  (ปล่อยว่าง ไม่ default เป็น easy ด้วย) ให้โมเดลรายงานความยากจริงเอง — pin ทาง
  ใดทางหนึ่งจะทำให้ coverage gate reject โจทย์ที่โมเดลเขียนถูกแต่ความยากไม่ตรง
  กับที่ pin ไว้

เทสยืนยันสด (live, Thai biology, เนื้อหาเดียวกัน): analysis-easy 3/3,
analysis-medium 3/3, analysis-hard 3/6 (ยิ่งยากยิ่งผ่านยาก ตามที่ตั้งใจ)

benchmark preset ใหม่: `--benchmark recall|understanding|analysis-easy|analysis-medium|analysis-hard`
(`analysis` เดิมยังใช้ได้ เป็น alias ของ `analysis-hard`) `--benchmark all`
รันครบ 9 case แล้ว

## บั๊กที่เจอและแก้ (session 2026-08-06 ต่อเนื่อง)

เจอทั้งหมดจาก live benchmark ข้ามวิชา (physics/algebra/economics/chemistry/
Thai biology) ไม่ใช่จาก code review เฉย ๆ — ทุกตัวมี unit test ปักหมุดพฤติกรรม
ที่ผิดไว้แล้ว:

1. **rounding tolerance เป็น absolute ไม่ใช่ relative**
   (`examgen/model/calc.go`, `choiceMentionsNumber`/`isLosslessRounding`)
   epsilon เดิม `5e-4` แบบ absolute ใช้ได้แค่ magnitude ~1-10 — ปัดเลข
   `1.67→"1.7"` หยาบเกินไปแต่ผ่าน ในขณะที่ `69.9%` จาก `69.914778` (3 sig fig
   ปกติ) กลับไม่ผ่านเพราะ scale ต่างกัน แก้เป็น relative tolerance `1e-3`
   (~3 sig figs) สม่ำเสมอทุก magnitude ปัดหยาบกว่านั้นต้องมีคำว่า "≈"/"about"/
   "ประมาณ" กำกับในตัวเลือกเอง
2. **superscript exponent parser พังเงียบ** (`examgen/model/calc.go`,
   `parseExponent`) เลขยกกำลัง superscript ¹²³ อยู่ Unicode block Latin-1
   Supplement (U+00B9/B2/B3) ส่วน ⁰⁴⁵⁶⁷⁸⁹ อยู่ block Superscripts-and-
   Subscripts (U+2070, U+2074-9) — โค้ดเดิมทำเลขคณิต `rune - '⁰'` สมมติว่าอยู่
   block เดียวกัน ผลคือ `²` คำนวณออกมาเป็นเลขชี้กำลัง **-8126** แบบเงียบ ๆ
   (ไม่ error, return ok=true) เจอจากคำตอบ titration เคมีที่เขียน
   `9.6 × 10⁻³ M` แก้ด้วย lookup table ตรง ๆ กระทบทุกวิชาที่ใช้ scientific
   notation ไม่ใช่แค่เคมี
3. **`coverage_contract` gate ลงโทษ draft ที่ตรวจสอบได้มากกว่า**
   (`examgen/evidence/evidence.go`, `gateSetCoverage`) เดิมเช็ค
   `slot.RequiresCalculation != q.NeedsCalculation()` แบบสมมาตรสองทาง —
   draft ที่อาสาคำนวณเกินที่ slot ขอ (คำนวณถูก ตรวจสอบได้) ถูก reject เพราะขัด
   contract ในขณะที่ draft พี่น้องที่หลบไม่ประกาศ calc เลย (ไม่มีใครตรวจเลขให้)
   กลับผ่าน แก้ให้ reject แค่ทิศทางที่ซ่อนการตรวจสอบที่จำเป็น (slot ต้องการ
   calc แต่โจทย์ไม่ให้)
4. **calc-slot เลือกไม่ฉลาดสำหรับ topic ที่คำตอบเป็นสัญลักษณ์**
   (`examgen/evidence/evidence.go`, `shouldDowngradeCalculation`) เดิมเช็คแค่
   "claim มีตัวเลขมั้ย" — กฎ exponent อย่าง `b^-7` หรือ `θ^-7` มีตัวเลข (เลขชี้
   กำลัง) แต่คำตอบธรรมชาติเป็นสัญลักษณ์ ไม่ใช่ตัวเลข นี่คือสาเหตุที่พีชคณิต
   calculation case เคยได้แค่ ~33% เพิ่ม `algebraicVariableExponentPattern`
   (regex จับ `\p{L}` + `^`/`**` + เลข และ superscript unicode ตรง ๆ) กัน atom
   แบบนี้ไม่ให้ถูกนับว่า calc-eligible

ผลหลังแก้ (เคมี titration, calculation case, เนื้อหาเดิมทุกรอบ):
11.1% → 25.0% (แก้ superscript) → 50.0% (แก้ rounding tolerance) ที่เหลือ 50%
ตอนนี้เป็นความผิดพลาดจริงของโมเดลเอง (คำนวณ multi-step titration ผิด) ไม่ใช่
gate หรือ code bug แล้ว

**ยังไม่ live-verify ซ้ำ**: bug 1 (rounding) กับ bug 3 (coverage_contract
asymmetry) มี unit test ครบแต่ยังไม่เจอ live run ที่ exercise เส้นทางนั้นตรง ๆ
อีกครั้งหลังแก้ (รอบ physics rerun หลังแก้บังเอิญไม่มี draft ไหนอาสา calc เกิน
slot เลย)

## Gate กับ semantic quality

Deterministic gate ตรวจสิ่งที่เครื่องยืนยันได้:

- schema, source role, exact quote, atom/chunk provenance
- slot coverage, skill/difficulty/operation และ calculation flag
- arithmetic/unit เมื่อข้อมี calculation payload
- duplicate และ heuristic ขั้นต่ำของ application/hard

Gate ไม่ได้ตอบว่าโจทย์ “ยากจริง”, ตัวลวงสมจริง หรือใช้ reasoning หนักพอหรือไม่
ส่วน `QualityGrader` เป็น semantic reviewer ระดับชุดแบบ advisory ใช้ช่วยเลือก
candidate เมื่อมีหลายชุด ไม่ใช่หลักฐานอิสระและไม่ควรถูกนับเป็น gate pass

รายงานต้องแยกสามชั้นเสมอ:

1. `drafts` — output ทุก first attempt และ bounded repair
2. `ship-ready` — ข้อที่ผ่าน deterministic QC และ contract
3. `semantic review` — groundedness, correctness, distractor quality และ
   difficulty fit จาก reviewer อิสระหรือ human sample

## ผล smoke ล่าสุดหลังแยก calculation flag

เป็นการตรวจ schema + runtime ด้วย DeepSeek, candidate เดียว, parallel 1 ไม่ใช่
benchmark สำหรับอ้างชัยชนะเหนือ NotebookLM:

| ชุด | ผล | calls | เวลา |
|---|---:|---:|---:|
| Scientific Notation, numeric | 2/4 ship-ready; 2/4 numeric verified | 15 | 1m43s |
| The Social Construction of Health, application-hard | 3/4 ship-ready; 0/0 ต้องคำนวณ | 7 | 29.5s |

ข้อสรุปที่เชื่อถือได้จากรอบนี้คือ schema ใหม่ทำงานและ gate กัน arithmetic ที่ผิด
ได้ ไม่ใช่ข้อสรุปว่าคุณภาพ semantic สูงพอแล้ว

### ผล smoke ข้ามวิชา (session 2026-08-06 ต่อเนื่อง, หลังแก้บั๊กทั้ง 4)

`--benchmark all` บน Thai biology (บทกระเพาะอาหาร, เนื้อหาเดียวกันทุก case,
DeepSeek, candidate 3, parallel 1):

| skill × difficulty | ผล |
|---|---:|
| recall | 2/2 (100%) |
| understanding | 5/5 (100%) |
| application easy/medium/hard | 4/4, 4/4, 3/3 (100% ทั้งหมด) |
| analysis easy/medium | 3/3, 3/3 (100% ทั้งคู่) |
| analysis hard | 3/6 (50%) |
| calculation | skip ถูกต้อง (บทนี้ไม่มีเนื้อหาตัวเลข) |

วิชาอื่นที่ทดสอบรอบนี้ (แยก run ไม่ใช่ matrix เดียวกัน):

| วิชา | case | ผล |
|---|---|---:|
| ฟิสิกส์ (EN) | application-hard rerun หลังแก้ bug 3 | 5/5 (100%) |
| เศรษฐศาสตร์ (EN, elasticity, วิชาใหม่) | calculation | 5/5 (100%) |
| เคมี (EN, titration, วิชาใหม่) | calculation | 50% (ดูหัวข้อบั๊กด้านบน) |
| ฟิสิกส์ (EN) | analysis-hard (skill ใหม่) | 5/5 (100%) |

**ยังไม่ได้ทดสอบเลย**: US History (`samples/openstax-us-history.pdf`,
130MB, ดาวน์โหลดไว้แล้วแต่ outline ไม่เคย compile) — เป็นตัวแทน pure-qualitative
humanities ตัวเดียวที่ session นี้หาไว้ และ sociology (
`.scratch/university-sources/sociology-3e.pdf`) ก็ไม่ได้รันเช่นกัน ทั้งสองไม่มี
หลักฐานอะไรเลย ไม่ใช่ "ผ่านแล้วลืมบันทึก"

## Graph compile และ batching

ค่า default ปัจจุบันคือไม่เกิน 4 chunks หรือประมาณ 8,000 runes ต่อ compile request
และ `max_tokens=4,096` สำหรับ DeepSeek

ผล A/B จาก source เดียวกัน:

| profile | calls | input tokens | output tokens | atoms | chunks ที่มี atom | เวลา |
|---|---:|---:|---:|---:|---:|---:|
| 4 chunks / 8k | 53 | 162,010 | 103,339 | 687 | 191 | 554.7s |
| 8 chunks / 16k | 33 | 131,073 | 106,930 | 413 | 139 | 558.0s |

8 chunks ลด calls แต่สูญเสีย atom และ chunk coverage มาก โดยเวลาแทบไม่ลด จึงยัง
ไม่ promote เป็น default และยังไม่ลอง 12 chunks. รอบถัดไปควรวัด output truncation,
retry, atom recall และ grounded quality ก่อนเปลี่ยน batching

**เพิ่ม auto-widen retry (session 2026-08-06 ต่อเนื่อง, `llm/generation/generator.go`)**:
bisection ลง chunk เดียวเดิมมีอยู่แล้วแต่ retry ภายใน (ทั้งใน bisection และใน
DeepSeek client เอง) ยิง token budget เท่าเดิมซ้ำ — ถ้า chunk เดียวความหนาแน่น
สูงพอที่จะ fail ที่ budget ปกติ (4,096) มันจะ fail ซ้ำเหมือนเดิมจนกู้ไม่กลับ (นี่
คือสาเหตุที่พีชคณิต 70 หน้าเคย fail แบบกู้ไม่ได้) เพิ่ม fallback: chunk เดียวที่
fail ที่ budget ปกติ ลองอีกทีที่ budget กว้างขึ้น (8,192, context window ขยาย
12,288→20,480) ก่อนยอมแพ้จริง ยืนยันแล้วว่า path นี้ยิงจริงใน production (เจอ
error message ระบุ `(widened retry)` ตรง ๆ ตอนเคมี 224 chunks ล้มเพราะ DNS
ชั่วคราว ไม่ใช่ truncation — งบประมาณกว้างขึ้นทำงานตามที่ตั้งใจ ปัญหาที่แท้จริง
รอบนั้นเป็น network blip ภายนอก)

## สิ่งที่ยังค้าง

1. ทำ extraction diagnostics ให้เห็น table/figure/scanned-page failure ก่อน
   ปล่อยให้ graph หรือ writer แก้ปัญหาที่ต้นเหตุผิดชั้น
2. ลด draft waste ด้วย prompt ที่บังคับ well-formed output, operation และ quote
   ให้ครบตั้งแต่ครั้งแรก; ใช้ repair เฉพาะ failure ที่ deterministic
3. อ่านข้อ easy/medium/hard จริงข้ามวิชา และเทียบกับ NotebookLM จาก source เดียวกัน
   โดยแยก provenance, correctness, reasoning depth และ distractor quality
4. ทำ semantic calibration กับ reviewer/model ที่ไม่ใช่ generator หรือ human sample
   ขนาดเล็ก ก่อนประกาศ pass rate เป็นคุณภาพ
5. วัด token/call ต่อ stage ทุก benchmark; ยังไม่เพิ่ม planner หรือ per-question
   judge จนกว่าจะพิสูจน์ว่าคุณภาพต่อ token ดีขึ้นจริง
6. **commit code ของ session 2026-08-06 ต่อเนื่อง** — ยังไม่ commit เลย
   (9 ไฟล์แก้ ครอบคลุม bug fix 4 ตัว + analysis skill tier + benchmark preset
   ใหม่) พิจารณาแยก commit ตาม concern (bug fix / analysis tier / preset) แทน
   commit เดียวรวด
7. ทดสอบ US History (`samples/openstax-us-history.pdf`) และ sociology
   (`.scratch/university-sources/sociology-3e.pdf`) — ดาวน์โหลด/มีไฟล์แล้วแต่
   ไม่เคยรัน US History คือตัวแทน pure-qualitative humanities ตัวเดียวที่ยังไม่
   มีหลักฐานว่า `analysis`/gate อื่น ๆ ทำงานเมื่อไม่มี causal/quantitative
   content แบบ STEM เลย
8. แก้ `--scope` ให้ route ไปถึง `GenerationDirective` จริง — ตอนนี้ผ่านได้แค่
   `--benchmark <preset>` เท่านั้น ผู้ใช้ทั่วไปสั่ง "ขอ hard application" ผ่าน
   `--scope` เฉย ๆ ยังทำไม่ได้ (เจอ bug นี้ตั้งแต่ session ก่อน ยังไม่แก้)
9. วัด cost tradeoff ของ `analysisSupportAtomIDs` ที่ข้าม atom ที่
   `supportsForm(atom,"calculation")` โดยตั้งใจ (กัน numeric-slot assignment
   แย่ง atom เดียวกัน) — ยังไม่วัดว่ากระทบ yield ของ analysis slot บนวิชาที่
   คำนวณเยอะ (ฟิสิกส์/เคมี) มากกว่าวิชาที่ไม่ค่อยมีเลข (ชีวะ/สังคม) แค่ไหน
10. live-verify ซ้ำ bug 1 (rounding tolerance) กับ bug 3 (coverage_contract
    asymmetry) — มี unit test ครบแต่ยังไม่เจอ live run ที่ exercise เส้นทางนั้น
    ตรง ๆ อีกครั้งหลังแก้

## เกณฑ์รอบถัดไป

ห้ามตัดสินจาก gate summary อย่างเดียว ให้เก็บอย่างน้อย:

- first-attempt drafts, bounded repairs และ ship-ready
- provider calls, input/output tokens และ latency แยก graph/generation/review
- quote/coverage/operation/calculation failures
- สัดส่วน application ที่ changed condition จริง
- สัดส่วน hard ที่มี linked steps และตัวลวงที่ไม่หลุดด้วยการแทนค่าตรง ๆ

ไฟล์อ้างอิง:

- `backend/prototype-exam-quality/VERDICT.md` — verdict และผลวัดประวัติศาสตร์
- `docs/research/exam-quality-research-2026-08.md` — research/เหตุผลของ design
- `backend/prototype-exam-quality/README.md` — วิธีรันและ option ปัจจุบัน
