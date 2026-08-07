# Handoff — exam-quality prototype

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
