# Handoff — exam-quality prototype

## Session 2026-08-08 — difficulty เป็น 3 แกน + distractor ที่ตรวจได้

### แก้ความเข้าใจผิดใน "สิ่งที่ยังค้าง" ข้อ 14

ข้อ 14 เขียนว่า calculation preset ปล่อย `slot.Skill` ว่าง **ผิด** ความจริงคือ
`buildCoverageContract` แจก skill ด้วย rotation `preferred[len(slots)%3]` แล้ว
`coverage_gate.go` บังคับให้โมเดล copy ตาม โมเดลเบี่ยง = ข้อโดน reject

หลักฐาน: 4 run บน physics เดียวกัน (`.scratch/e0312e3e107a1953/`) ได้ skill
sequence เหมือนกันเป๊ะ (S01 analysis, S02 application, S03 recall, S04 recall,
S05 understanding) ขณะที่ expression และ difficulty เปลี่ยน — skill ถูกกำหนดโดย
ตำแหน่ง slot ไม่ใช่เนื้อหา

`prompt.go` step 5 ("for a slot whose skill is not fixed above") จึงเป็น dead
instruction — ไม่มี calc slot ไหน unfixed เลย นี่คือเหตุผลจริงที่ prompt
experiment 2 รอบไม่ขยับ ไม่ใช่ "โมเดล map เกณฑ์ไม่ได้"

### สิ่งที่เปลี่ยน

`CoverageSlot` มี 3 แกน difficulty เป็นค่า derive ไม่ใช่ค่าที่พิมพ์เอง:

```text
MinDepth        จำนวน source claim ที่คำตอบต้องใช้จริง (ไม่ใช่ reasoning_steps ที่โมเดลเขียนเอง)
MinDecoys       ค่าใน stem ที่วิธีทำไม่ใช้
Discrimination  low | high — ตัวลวงต้องใกล้เฉลยแค่ไหน

easy   = ไม่ยกแกนไหนเลย
medium = ยกแกนใดแกนหนึ่ง
hard   = ยกทั้งสามแกน
```

`difficulty` ถูก pin เฉพาะเมื่อ run สั่ง ไม่งั้นปล่อยว่างให้โมเดลรายงานจริงแล้ว
gate เช็คแกนแทนคำ (กติกาเดิมของ analysis ขยายไปทุก skill) `difficulty="easy"`
ที่ hardcode ให้ recall/understanding **ถูกลบ**

field ใหม่ที่ประกาศแล้ว Go ตรวจ (ท่าเดียวกับ `Calculation`):

- `choices[].distractor_expression` — เลขผิดที่ eval ได้จริง ต้องตรงกับตัวเลขใน
  ช้อยและต่างจากเฉลย
- `choices[].distractor_atom_id` — สำหรับวิชาไม่มีเลข: ช้อยผิดคือ claim จริงจาก
  ที่อื่นในเนื้อหา ต้อง resolve ได้และห้ามเป็น atom ของ slot เอง
- `decoy_values` — ต้องอยู่ใน stem จริงและไม่อยู่ใน expression
- `flawed_expression` — skill `error-finding` ใหม่: stem โชว์วิธีทำผิด Go พิสูจน์ว่าผิดจริง
- gate ใหม่: `distractor_path`, `decoy_values`, `flawed_work`

candidate selection ตัดสินด้วย `AxisTally` (ของที่ Go ตรวจแล้ว) **ก่อน** เรียก
LLM grader — grader ถูกเรียกเฉพาะตอนเสมอทั้ง acceptance และ axis

### ผลวัดจริง (DeepSeek, `--set-candidates 3 --budget 5`)

physics `openstax-physics.pdf 140-220` / economics `economics-3e.pdf 60-150`

| case | baseline เดิม | run แรก | สอง sample สุดท้าย (โค้ดเดียวกัน) |
|---|---|---|---|
| physics calculation | 5/5 (100%) | 2/8 (25%) | 3/8, 4/6 |
| physics error-finding | — | 2/10 | 4/7, **5/7** |
| physics recall-hard | — | 1/10 (10%) | 5/6, 5/7 |
| econ recall | 5/5 (100%) | 5/6 (83%) | 5/6, 5/6 |
| econ recall-hard | — | 2/8 (25%) | 4/10, 5/9 |
| econ understanding | — | 5/5 (100%) | 5/6, 5/5 |

**อ่าน pass rate ผิดมาตลอด** — ตัวที่นิ่งคือ **accepted count** ไม่ใช่ accepted/drafts
budget=5 และ accepted ลงที่ 4-5 แทบทุก case ทุก run ส่วน drafts แกว่ง 5-10 เพราะ
retry ยิงบ้างไม่ยิงบ้าง pass rate จึงแกว่งตาม denominator ไม่ใช่ตามคุณภาพ
ควรรายงานเป็น **accepted/budget** (คลุม contract ได้แค่ไหน) คู่กับ **drafts** (ราคา)

axis tally ของ run สุดท้าย: physics recall-hard = atom-backed 15, decoys 5,
multi-claim 5 · econ recall-hard = atom-backed 9, decoys 4, multi-claim 4 ·
physics calculation = error-paths 9 · physics error-finding = flawed-work 2

**recall-hard เจนได้จริงทั้งสองวิชา** คือสิ่งที่ single-axis model ทำไม่ได้

`calculation` accepted count ทั้ง session: 2, 2, 3, 3, 3, 4, 2, 3 บน draft 6-10
— **noise กลบทุก effect** อย่าอ่านเป็น before/after ของอะไรทั้งนั้น เทียบกับ
baseline 5/5 คือ acceptance ต่ำลงแต่ของที่ผ่านหนักกว่าเดิมมาก (error-paths 9 vs 0)
ที่เหลือ fail คือ gate เก่า 2 ตัว (`demand_contract` ขาด changed_condition,
`not_a_duplicate`) + โมเดลพลาด `distractor_expression` เอง

### บทเรียนที่แพงที่สุดของ session นี้

`OpenAIClient.ChatJSON` **รับ schema แล้วไม่ส่ง** — ตั้งแค่
`response_format={"type":"json_object"}` บน DeepSeek (และ OpenAI-compatible
ทุกเจ้า) schema ไม่ constrain อะไรเลย ทำ field เป็น `required` ในนั้น = ไม่มีผล
(วัดแล้ว 2 รอบ ได้ 0 ทั้งคู่)

สิ่งที่โมเดลตามจริงคือ **JSON template ท้าย `questionSystem`** — ย้าย field ไป
ใส่ใน template รอบเดียว: verified error paths 0 → 9, atom-backed 0 → 15

ผลข้างเคียงที่ตามมาทันที: template ที่ลืมใส่ `calculation` object ทำให้โมเดล
เลิกส่ง calculation ทั้งชุด — **template คือ contract จริง ไม่ใช่ตัวอย่าง**

### สาม residue สุดท้าย — แก้แล้ว 2 เต็ม 1 ครึ่ง

| residue | ก่อน | หลัง |
|---|---|---|
| `"according to the passage"` | 1 ข้อ/run ทุก run 6+ รอบ | **0** ทั้งสองวิชา 3 run ติด |
| error-finding copy สมการถูก | 2-3 ข้อ/run | **0** — flawed-work verified 4/6 |
| econ recall-hard decoy ไม่อยู่ใน stem | 4 fail, accepted 3/9 | 2 fail, accepted 4/7 |

ทั้งสามใช้ท่าเดียวกัน: **บอกลำดับการทำ ไม่ใช่บอกข้อห้าม**
- ห้าม "according to the passage" เฉย ๆ = โมเดลไม่มีทางออกเมื่อ stem ขาดข้อมูล
  เปลี่ยนเป็น "วลีนี้แปลว่า stem ขาดข้อมูล — ใส่ข้อมูลเข้าไป" + ตัวอย่าง before/after
- "break it once" บน atom แปลงหน่วย operation เดียว = โมเดลอ่านว่าทำไม่ได้
  บอกรูปแบบตรง ๆ ว่าคูณแทนหารคือ mistake ที่พบบ่อยสุด
- decoy: เลือกจากเนื้อหาแล้วหวังว่า stem จะพูดถึง = กลับด้าน
  เปลี่ยนเป็น เขียน stem ที่มีรายละเอียดเกิน 1 ตัวก่อน แล้วค่อยอ่าน decoy ออกจาก
  stem ที่เพิ่งเขียน + ย้ำว่ารายละเอียดอยู่ใน stem ห้ามอยู่ในช้อย (รอบแรกที่ไม่ย้ำ
  โมเดลไปยัดในช้อยจน `checkChoiceLengthSkew` ยิง)

`RepairStemSelfContainment` ตัดวลีชี้ทิ้งเมื่อมันเป็นแค่ lead-in และคำถามข้างใต้
ยืนได้เอง — วลีที่ฝังกลางประโยค ("the value in this table") ยัง fail ต่อ เพราะ
stem นั้นพึ่งของที่นักเรียนมองไม่เห็นจริง ๆ

### ที่ยังไม่ได้ทำ / รู้แล้วว่าเป็นปัญหา

- `distractor_atom_id` ที่ชี้ atom ของ slot เอง: prompt แก้แล้วไม่หาย จึงใช้
  `RepairDistractorAtoms` ตัด citation ทิ้งแทน reject ทั้งข้อ (defect อยู่ที่
  label ไม่ใช่ตัวคำถาม) — แก้แล้ว econ recall 5/10 → 5/6
- `"according to the passage"` ยังหลุดประปราย 1 ข้อ/run
- prompt dedupe: ทำแล้ว system prompt 3,211 → 2,902 token (−10%) ไม่ทิ้งกฎไหน
  `QuestionSetSystem` เคยพูดซ้ำ slot protocol เกือบทั้งบล็อก (~300 token ส่งซ้ำ
  ใน request เดียว) — อันตรายกว่าเปลืองคือแก้ protocol แล้วลืมบล็อกนี้ โมเดลจะ
  ถือ contract 2 เวอร์ชัน ส่วน schema description **ไม่ต้องตัด** เพราะไม่ได้ส่งอยู่แล้ว
  ผลคุณภาพหลัง dedupe: recall/recall-hard/understanding เท่าเดิม, calculation
  อยู่ในช่วง noise เดิม
- discrimination ที่ derive เอง = advisory แล้ว (`DiscriminationEnforced()`)
  บังคับผ่าน gate เฉพาะ slot ที่ run pin difficulty มา — gate ลบข้อได้อย่างเดียว
  ยกบาร์ไม่ได้ บาร์อยู่ที่ prompt + candidate selector
- ตัวลวงเทียบตัวเลขด้วย tolerance 2% (`distractorRounding`) ไม่ใช่บาร์ของเฉลย
  — "0.60 N" กับ 0.5988 คือตัวเดียวกันสำหรับตัวลวง
- `error-finding` แก้แล้ว **0/9 → 3/9** (flawed-work verified 3, atom-backed 4)
  สองสาเหตุคนละเรื่องกัน:
  1. บั๊กของ gate เอง — ช้อยของ error-finding เป็น**คำวินิจฉัย ไม่ใช่ตัวเลข**
     `distractor_expression` จึงหาเลขในช้อยไม่เจอ ล้ม 10/10
     `RepairErrorFinding` ตัดทิ้ง แล้วให้ตัดสินด้วย `distractor_atom_id`/reasons
     เหมือน prose item อื่น
  2. โมเดลเข้าใจโจทย์กลับด้าน — เล่า**วิธีทำที่ถูก** แล้วถามว่าผิดตรงไหน
     ("5 N × 0.225 lb/N = 1.125 lb. What is wrong with this work?" — ไม่ผิดเลย)
     prompt สั่งลำดับการสร้างแทน: เขียนสมการถูกก่อน → พังมัน 1 จุด → ค่อยเขียน
     stem รอบสมการที่พังแล้ว + บอก trap ตรง ๆ พร้อมวิธีเช็คตัวเอง
  `RepairErrorFinding` ยังกู้สมการผิดจาก equation ที่ stem โชว์เองได้ (regex
  `A op B = C` + eval ทั้งสองข้าง) ตอนโมเดล copy สมการถูกลงทั้งสองช่อง — แต่ถ้า
  stem โชว์งานที่ถูกอยู่แล้วก็กู้ไม่ได้และ fail ต่อ ซึ่งถูกแล้ว
- symbolic key แก้แล้ว — วัดขนาดปัญหาก่อน: **4 ครั้งทั้ง repo, 2 รูปแบบ, จาก run
  เดียว** เล็กกว่าที่คิดมาก
  `NormalizeExpressions` แก้เฉพาะที่แก้ได้โดยไม่เปลี่ยนความหมาย: Arith รับ `×`
  `÷` อยู่แล้วแต่ไม่รับ `−` (typographic minus) `–` `·` `⁄` และรับ symbolic
  lead-in ไม่ได้ (`a = F_net / m = 100/5` → เอา segment ขวาสุดที่ parse ผ่าน)
  **ไม่กู้** `Fnet/m` / `4T-f` เพราะไม่มีตัวเลขเลย — ใส่ค่าให้เอง = แต่งโจทย์
  ไม่ใช่ซ่อม สิ่งที่กำจัดสองอันนั้นคือ prompt บรรทัดใต้ JSON template (จุดที่ติด)
  ที่ระบุชื่อ string พวกนั้นตรง ๆ ว่าห้ามเขียน
  Arith เองไม่แตะ — มันต้อง re-eval expression ที่เก็บใน DB ไปอีกหลายปี การที่มัน
  ไม่มี identifier เป็นคุณสมบัติที่ควรรักษาไว้ fold จึงอยู่ข้างนอก
- error-finding ล่าสุด **4/7** (flawed-work verified 4, atom-backed 3) และ
  `arithmetic` fail หายเกลี้ยงในรอบเดียวกัน trajectory: 2/9 → 0/9 → 3/9 → 4/7
  ที่เหลือคือ atom แปลงหน่วย N→lb ซึ่งมี operation เดียว โมเดลไม่ยอมพังค่าคงที่


อัปเดต 2026-08-07 บน branch `prototype/exam-quality`, working tree clean — ทุกโค้ด
commit แล้ว รวม extraction diagnostics (`f81309e`) และ prompt skill-choice/
distractor-dedup experiment (commit ถัดจากนี้ ดู `git log --oneline -5` สำหรับ
hash ล่าสุด)

## CLI ปัจจุบัน

- `--set-generation` / `--graph-compile` / `--question-plan` / `--per-chunk`
  **ถูกลบ** — graph-backed set generation เป็นเส้นทางเดียว
- `--deepseek-host` / `--deepseek-api-key` **ถูกลบ** → `--base-url` / `--api-key`
  (`LLM_API_KEY`). `--provider deepseek` ยังใช้ได้เป็น preset, ยังอ่าน
  `DEEPSEEK_API_KEY`
- `--stop-on-full-set` (default off), `--provider openai --base-url` สำหรับ
  provider ใดก็ได้ที่พูด OpenAI wire format รวม local server
- `--benchmark` รองรับ comma-list (`"recall,understanding,application-hard"`)
  เพื่อข้าม case ที่ไม่เหมาะกับ source
- outline cache pin ที่ `outline-v4` — cache เก่าไม่มี atom รันต่อไม่ได้ (รอบแรก
  หลัง pull จ่าย pass 1 ใหม่หนึ่งครั้ง ไม่ใช่ regression)

คำสั่ง benchmark:
`protoexam.exe --provider deepseek --pdf <pdf> --pages <range> --benchmark all --set-candidates 3 --parallel 1`

## Contract ปัจจุบัน

`calculation` ไม่ใช่ค่าใน `skill` (เป็นวิธีดำเนินการ ไม่ใช่ cognitive demand):

```text
skill: recall | understanding | application | analysis
difficulty: easy | medium | hard
requires_calculation: true | false
calculation: { expression, expected, unit }  # ต้องมีเมื่อ flag เป็น true
```

`application + hard + requires_calculation=true` และ
`understanding + easy + requires_calculation=true` ทั้งคู่ถูกต้อง `--force-calc`
บังคับเฉพาะ flag ไม่ใช่ skill/difficulty JSON เก่า (`skill: calculation`) ยังอ่านได้
เป็น compat alias → canonicalize อัตโนมัติ ไม่ควรสร้างใหม่ด้วยรูปแบบเก่า

### `analysis` (skill)

`easy`/`medium` ต้องการ `reasoning_steps` ≥ 2 ขั้น, `hard` ≥ 3 ขั้น
supporting atom ต้องมาจาก chunk คนละก้อน + relation type คนละแบบกับ atom หลัก
(`analysisSupportAtomIDs`, `examgen/evidence/evidence.go`) coverage contract
**ไม่ pin difficulty ของ analysis slot** — ปล่อยให้โมเดลรายงานความยากจริงเอง
(pin ทางใดทางหนึ่งทำให้ gate reject โจทย์ที่เขียนถูกแต่ยากไม่ตรง pin)

`analysis` ไม่ผูกกับ `hard` เสมอ (แก้จาก hardcode เดิมตามคำท้วงของผู้ใช้ว่าไม่มี
เหตุผลรองรับ Bloom's — analyze เป็นประเภทการคิด ไม่ใช่ระดับความยาก)

Preset: `--benchmark recall|understanding|analysis-easy|analysis-medium|analysis-hard`
(`analysis` alias ของ `analysis-hard`) `--benchmark all` ครบ 9 case

## Pipeline

```text
PDF → extraction diagnostics + content chunks → graph/outline compile
  → lesson + coverage contract → contract preflight → slot-local evidence packet
  → set generation → deterministic QC → optional advisory semantic/set review
  → report: drafts / ship-ready / review
```

## สิ่งที่ runtime ใช้อยู่ตอนนี้

- Graph compile ส่งเฉพาะ content chunks ปกติ; `KeepAllTopics` สำหรับ regression/audit
- Evidence เลือกแบบ slot-local (atom หลัก + quote/chunk ที่อ้างถึง + neighbor
  ที่จำเป็น) ไม่ส่ง evidence pool ทั้งบทเข้า writer
- Context จัดอันดับตาม slot/atom ก่อน document order, จำกัดขนาดกัน evidence drift
- Contract preflight ซ่อมได้เฉพาะ defect ที่ deterministic (atom/quote/chunk
  ไม่ตรงกัน) ไม่สร้าง evidence ใหม่แทนโมเดล
- Application/medium/hard ต้องมีสัญญาณเปลี่ยนเงื่อนไขหรือ linked operation;
  hard ต้องมี support evidence + reasoning ≥ 2 ขั้นเมื่อ source รองรับ
- `Operation` ต้องตรงกับ slot, ข้อต้องอ้าง evidence ในแพ็กเก็ตของมันเอง
- Calculator ถูกเรียกตาม slot ที่ต้องคำนวณเท่านั้น
- Bounded retry เฉพาะ missing/failed slots; ไม่มี per-question judge call ใหม่
- `QuestionSetPrompt`: lesson → source context (byte-identical ข้าม candidate/case
  ของ lesson เดียวกัน) → directive → coverage contract → evidence packet →
  rejection memory → candidate marker → slot protocol — เรียงแบบนี้เพื่อให้ตรง
  provider prefix cache (ยืนยัน generate-set cached-input ~65-98% ใน live run)
- `gateDistinct` จับ duplicate ระดับ operation (normalize expression, เทียบ
  operator/operand structure) ไม่ใช่แค่เทียบ stem text — จับ `5*0.225` vs
  `2*0.225` แต่ไม่จับข้าม operation จริง (`5*0.225` vs `3*9.8`)

## Gate กับ semantic quality

Deterministic gate ตรวจ: schema, source role, exact quote, atom/chunk
provenance, slot coverage, skill/difficulty/operation/calculation flag,
arithmetic/unit, duplicate, heuristic ขั้นต่ำของ application/hard

Gate **ไม่ตอบ** ว่าโจทย์ยากจริง/ตัวลวงสมจริง/reasoning หนักพอ `QualityGrader`
เป็น semantic reviewer ระดับชุดแบบ advisory ช่วยเลือก candidate เท่านั้น
ไม่ใช่หลักฐานอิสระ ไม่นับเป็น gate pass

รายงานแยกสามชั้นเสมอ: `drafts` (ทุก attempt) → `ship-ready` (ผ่าน deterministic
QC) → `semantic review` (groundedness/correctness/distractor/difficulty fit
จาก reviewer อิสระหรือ human sample — ยังไม่มี)

## Graph compile batching

Default: ≤4 chunks หรือ ~8,000 runes ต่อ compile request, `max_tokens=4,096`
(DeepSeek) 8 chunks/16k ลด calls (53→33) แต่เสีย atom (687→413) และ chunk
coverage (191→139) มาก โดยเวลาแทบไม่ลด → ยังไม่ promote เป็น default, ยังไม่ลอง
12 chunks

Auto-widen retry (`llm/generation/generator.go`): chunk เดียวที่ fail ที่ budget
ปกติ ลองอีกทีที่ budget กว้างขึ้น (8,192, context 12,288→20,480) ก่อนยอมแพ้ —
ยืนยันแล้วว่ายิงจริงใน production

## สิ่งที่ยังค้าง

1. ~~Extraction diagnostics ให้เห็น scanned-page/figure/table failure~~ —
   **เสร็จ code-level** (session 2026-08-07, ยังไม่ commit):
   - `checkExtraction` (`app/app.go`) list เลขหน้าจริงที่ text อ่อนแทนนับรวม
   - `ExtractDocling` (`pdfx/extract/docling.go`) เทียบจำนวน `![...]` ใน
     markdown กับ asset ที่ดึงได้จริงต่อหน้า → warn เมื่อรูปหาย
   - `raggedTableWarnings` (`docling.go`) เช็ค markdown table block ที่จำนวน
     cell ต่อแถวไม่เท่ากัน (รวม header-separator row) → warn ว่า table
     extraction อาจพัง (Docling แปลง table เป็น markdown text ตรง ๆ ไม่ใช่
     asset แยก จึงต้องเช็คจาก structure ของ text เอง)
   - ทั้งหมดพิมพ์ก่อน `confirm()` เสมอ ไม่ใช่แค่ `--extract-only`
   - unit test ครบทั้ง 3 สัญญาณ (`main_test.go`, `auto_test.go`), `go vet` +
     `go test ./...` ผ่านหมด, build clean

   **ยังไม่ live-verify กับ Docling จริง**: environment นี้ไม่มี
   `.scratch/docling-venv` (ต้องรัน `setup-docling.ps1` ซึ่งโหลด OCR model
   หลาย GB) เทสทั้งหมดอิง JSON payload จำลองผ่าน `stubDoclingRunner` ไม่ใช่
   Docling ตัวจริง — ยังไม่ยืนยันว่า heuristic เหล่านี้ trigger ถูกจังหวะกับ
   PDF ที่มีรูป/ตารางเสียจริง
2. ลด draft waste ด้วย prompt ที่บังคับ well-formed output/operation/quote ครบ
   ตั้งแต่ครั้งแรก; ใช้ repair เฉพาะ failure ที่ deterministic
3. อ่านข้อ easy/medium/hard จริงข้ามวิชา เทียบ NotebookLM จาก source เดียวกัน —
   แยก provenance, correctness, reasoning depth, distractor quality
4. Semantic calibration กับ reviewer/model ที่ไม่ใช่ generator หรือ human sample
   ขนาดเล็ก ก่อนประกาศ pass rate เป็นคุณภาพ
5. วัด token/call ต่อ stage ทุก benchmark; ยังไม่เพิ่ม planner/per-question judge
   จนกว่าจะพิสูจน์คุณภาพต่อ token ดีขึ้นจริง
6. **US History + sociology ยังไม่เคยรันเลย** — `samples/openstax-us-history.pdf`
   และ `.scratch/university-sources/sociology-3e.pdf` มีไฟล์แล้วแต่ไม่เคย compile
   outline เป็นตัวแทน pure-qualitative humanities เดียวที่ยังไม่มีหลักฐานว่า
   analysis/gate ทำงานเมื่อไม่มี causal/quantitative content แบบ STEM
7. `--scope` ไม่ route ไปถึง `GenerationDirective` จริง — ผู้ใช้สั่ง "ขอ hard
   application" ผ่าน `--scope` เฉย ๆ ยังทำไม่ได้ (มีมาตั้งแต่ก่อน session นี้)
8. ยังไม่วัด cost tradeoff ของ `analysisSupportAtomIDs` ที่ข้าม atom ที่
   `supportsForm(atom,"calculation")` โดยตั้งใจ — ไม่รู้กระทบ yield ของ analysis
   slot บนวิชาคำนวณเยอะ (ฟิสิกส์/เคมี) มากกว่าวิชาไม่มีเลข (ชีวะ/สังคม) แค่ไหน
9. Interactive path (`renderSummary`, `writeRun`) — session ล่าสุดรันแต่ทาง
   `--benchmark`, ยังไม่ live-verify
10. `--provider ollama` (default) ตอนนี้ต้อง compile evidence เสมอ (เดิม opt-in)
    ยังไม่ได้รันสดหลังแก้
11. calc-tool memoization key ใหม่ (lesson+packet แทน slot chunk set) ยืนยันแค่
    บางส่วน — ยังไม่มี live run ที่ exercise bounded repair path จริง (ต้องเป็น
    เคสที่ retry เกิดจริง เช่น analysis-hard หรือวิชาที่พลาดบ่อย) unit test
    ปักหมุดไว้แล้วใน `calctool_test.go`
12. `gateDistinct` operation-level duplicate ตรวจแล้วไม่ครอบคลุม cross-operation
    ที่จริงเป็นการวัดผลเดียวกัน (เช่น `5*0.225` vs `1/0.225` ไม่ถูก flag) —
    ยัง 5/5 ไม่มี yield regression แต่ไม่ครอบคลุม
13. Cross-case prefix-cache benefit ของการย้าย source context ยังไม่มีตัวเลข
    aggregate เต็ม (ต้องรัน 9-case บน lesson เดียวถึงจะเห็นผลรวม ~37
    generate-set calls ตามที่วัดเดิม)
14. **Independent semantic review ของข้อสอบจริง (session 2026-08-07)** — อ่าน
    314 ข้อจาก 12 benchmark run ล่าสุด (5 วิชา) เอง (ไม่ใช่ generator
    self-grade) เจอ 2 pattern เป็นระบบ:
    - `calculation` preset (ไม่มี `TargetSkill` — `slot.Skill` ปล่อยว่างโดย
      ตั้งใจ, `coverage_gate.go:62` ไม่บังคับ skill match เมื่อว่าง) ได้ skill
      ไม่สัมพันธ์กับความซับซ้อนจริงของสมการ (rocket-sled thrust แก้สมการหา
      ตัวแปร 3 ขั้น vs single-formula plug-in ได้ skill เดียวกันบ่อยครั้ง)
    - `analysis+easy+requires_calculation` บาง slot (physics F=ma) ผ่าน
      `demand_contract` ด้วย reasoning_steps ที่เป็นแค่ "rearrange formula" +
      "substitute" — โครงสร้างตื้นกว่า analysis slot อื่นในวิชาเดียวกัน
      (ไม่ถึงกับผิด gate ที่มีอยู่ แต่ semantic quality ไม่แน่นเท่า)
    - แก้ prompt 2 จุด (`distractor_reasons` ต้องไม่ซ้ำกันเองในข้อเดียวกัน,
      เกณฑ์ understanding-vs-application สำหรับ calc) แล้วรันเทียบ **สองรอบ**
      (physics `calculation,application-easy`, DeepSeek candidate 3): ผลไม่
      ขยับทั้งสองรอบ แม้ย้าย instruction จาก buried clause ในพารากราฟยาว
      (`benchmark.go`) ไปเป็น bullet เดี่ยวใกล้จุดตัดสินใจ (`prompt.go`
      slot execution protocol step 5) — สรุปได้ว่า **ไม่ใช่ปัญหาความยาว/
      ตำแหน่งของ prompt** โมเดลดูเหมือนไม่ map เกณฑ์ "plug-in vs isolate
      unknown" เข้ากับคำ understanding/application ตามที่ตั้งใจ ไม่ว่าจะวาง
      ตรงไหน
    - `not_a_duplicate` (N→lb cross-operation dedup, commit `d52b819`) ทำงาน
      เสถียรทั้ง 3 รอบวัด ไม่ใช่ regression จากการแก้วันนี้
    - `distractor_reasons` ซ้ำคำเป๊ะในข้อเดียวกัน: พบแค่ 2/314 ข้อ (ไฟล์เดียว)
      ก่อนแก้ — rare ไม่ใช่ systemic, แก้ prompt ไปแล้วแต่ sample เล็กเกินจะ
      ยืนยัน/ปฏิเสธว่าหายจริง
    - **แนะนำต่อ**: อย่าไล่แก้ skill-label ทาง prompt-only ต่อ (พิสูจน์แล้วว่า
      ไม่นิ่ง 2 รอบ) ทางที่ได้ผลจริงคือ deterministic post-hoc classify จาก
      `calculation.expression` operator count ฝั่ง Go แทนให้โมเดลเลือก
      (ปลอดภัยเพราะ `slot.Skill==""` ไม่ถูก gate บังคับอยู่แล้ว) —
      **ยังไม่ implement**: ต้องเช็คก่อนว่า non-benchmark path
      (`evidence.go`'s `buildCoverageContractForRun`) ปล่อย `slot.Skill` ว่าง
      สำหรับ calc-flagged atom เหมือน benchmark preset มั้ย ก่อนเขียนจริง

## เกณฑ์รอบถัดไป

ห้ามตัดสินจาก gate summary อย่างเดียว เก็บอย่างน้อย:

- first-attempt drafts, bounded repairs, ship-ready
- provider calls, input/output tokens, latency แยก graph/generation/review
- quote/coverage/operation/calculation failures
- สัดส่วน application ที่ changed condition จริง
- สัดส่วน hard ที่มี linked steps และตัวลวงที่ไม่หลุดด้วยการแทนค่าตรง ๆ

## ไฟล์อ้างอิง

- `backend/prototype-exam-quality/VERDICT.md` — verdict และผลวัดประวัติศาสตร์
  (ไม่รวมผลวัด session 2026-08-07 — ดู `git log -p -- docs/HANDOFF.md` สำหรับ
  ตัวเลขเต็มของ optimize session ถ้าต้องอ้างอิงย้อนหลัง)
- `docs/research/exam-quality-research-2026-08.md` — research/เหตุผลของ design
- `backend/prototype-exam-quality/README.md` — วิธีรันและ option ปัจจุบัน
- `tools/analyze_length_bias.py`, `tools/show_lenbias_examples.py`,
  `tools/render_liveverify_html.py` — เครื่องมือวิเคราะห์ length-bias/liveverify
- logs: `backend/prototype-exam-quality/.scratch/liveverify-*.log`
