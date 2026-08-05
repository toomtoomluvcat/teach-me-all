# Handoff — exam-quality prototype

อัปเดต 2026-08-06 บน branch `prototype/exam-quality` หลัง commit
`0d7985c` (`refactor: make calculation an orthogonal flag`)

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
skill: recall | understanding | application
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
