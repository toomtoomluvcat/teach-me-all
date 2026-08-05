# Handoff — exam-quality prototype

อัปเดต 2026-08-06 บน branch `prototype/exam-quality`.
เอกสารนี้เป็นสถานะปัจจุบันและแผนทดลองถัดไป; ผลวัดเก่าให้อ่านจาก
`backend/prototype-exam-quality/VERDICT.md` โดยระวังว่าแถว judge เก่าบางส่วนไม่ใช่
runtime path ปัจจุบันแล้ว

## สถานะปัจจุบัน

```text
PDF → Docling extraction → chunks
    → map/reduce outline + concept graph
    → optional evidence-atom compilation
    → lesson context + coverage contract
    → set generation
    → deterministic QC + optional advisory quality/set review
```

- Acceptance path ใช้ QC deterministic: โครงสร้าง, source role, exact quote,
  arithmetic, unit, duplicate, provenance และ coverage contract
- `QualityGrader` เป็น semantic reviewer ระดับชุด ใช้เป็นข้อมูลเลือก candidate
  ไม่ใช่ hard gate
- per-question `Judge`/`AddJudgeGates` ถูกถอดจาก runtime แล้ว; package เก่าเหลือไว้
  สำหรับการทดลองเฉพาะกิจ
- cleanup/runtime refactor ผ่าน `go test`, `go vet`, `go build`, Python self-test,
  Ruff และ Pyright แล้ว

## สถานะหลังรอบ research + cross-subject benchmark

รอบนี้ลงมือแก้ P0 ที่คุ้ม token สูงสุดแล้ว โดยไม่เพิ่ม planner หรือ per-question
judge call ใหม่:

- graph compiler ส่งเฉพาะ content chunks เมื่ออยู่ในโหมดปกติ และมี fallback
  แบ่ง batch เฉพาะเมื่อ response แตก/ถูกตัด
- set generation ใช้ **slot-local evidence packet**: atom หลัก, support atoms
  ที่จำเป็น และ raw chunks ของ slot ก่อน neighbor ที่เกี่ยวข้อง
- contract จัดอันดับ source chunks ตาม slot/atom ก่อน document order และ gate
  `Operation` ให้ตรงกับ slot
- application medium/hard ต้องระบุ `changed_condition` และ distractor rationale;
  hard ต้องมี support atom และ reasoning steps อย่างน้อย 2 ขั้น
- calculation ผูกกับ evidence ของ slot, ตรวจตัวเลขด้วย calculator และไม่รับ
  คำตอบ symbolic ที่ยังไม่ได้แปลงเป็นค่าตัวเลขใน choice
- DeepSeek ที่เห็น object แทน array ใน metadata ถูก normalize แบบ deterministic
  ก่อนเข้า gate; ไม่ได้ลดความเข้มของ gate

ความหมายของตัวเลขในรายงานใหม่แยกเป็น `drafts` กับ `ship-ready` แล้ว: `drafts`
รวม output จาก first attempt และ bounded repair; draft ที่เกิน/ไม่ตรง contract
จะยังนับเป็น draft แต่ไม่นับเป็นข้อพร้อมใช้ จึงไม่เอา pass rate ของ gate ไปอ้าง
เป็น semantic quality

## ผล hard/application ล่าสุดหลายวิชา

เป็น targeted rerun หลังเพิ่ม support atoms; ใช้ DeepSeek, candidate เดียว,
parallel 1 และอ่าน failure rows จริง ไม่ใช่ดู gate summary อย่างเดียว:

| วิชา | ship-ready / target | raw drafts | ผลที่อ่านได้ |
|---|---:|---:|---|
| คณิตศาสตร์ | 3/3 | 3 | hard มี linked operations หลายขั้น |
| ฟิสิกส์ | 3/3 | 3 | มีการเปลี่ยนเงื่อนไขและตัดสินหลายขั้น |
| ชีววิทยา | 2/2 | 2 | ใช้ causal/conditional evidence ได้ครบ |
| จิตวิทยา | 3/3 | 3 | application ผูกกับ source relationship |
| สังคมวิทยา | 3/3 | 4 | 1 draft ตกเพราะอ้าง “passage” ที่ผู้สอบไม่เห็น; operation enum กัน failure เดิมได้ |
| เศรษฐศาสตร์ | 3/3 | 5 | 2 draft ตกเพราะ quote ไม่ verbatim/operation ไม่ตรง contract |

จุดสำคัญคือ sociology/economics ไม่ควรสรุปว่า “คุณภาพ 60%”: สามข้อที่อยู่ใน
target และผ่านเป็น ship-ready ทั้งหมด แต่ generation efficiency ยังเสีย draft
สองข้อ และควรลด failure แบบนี้ในรอบถัดไปด้วย prompt/repair ที่เฉพาะจุด

Calculation targeted checks: คณิตศาสตร์ 3/3 และสังคมวิทยา 3/3 numeric verified;
ฟิสิกส์ 1/2 เพราะอีกข้อให้คำตอบเป็น radical (`20√3 m`) ซึ่งอาจถูกทางคณิตศาสตร์
แต่ไม่ตรง product contract ที่ต้องการค่าตัวเลขใน choice — gate จึงกันไว้โดยตั้งใจ

## ตอบคำถามเรื่องการอ่านข้อสอบ

gate ไม่ใช่ semantic grader และผมไม่ควรมั่นใจใน gate ขนาดนั้น: gate ตอบได้ว่า
“ข้อสอบทำตาม contract ขั้นต่ำหรือไม่” เช่น quote ตรงไหม, operation ตรงไหม,
ตัวเลขคำนวณได้ไหม, stem อ้างสิ่งที่มองไม่เห็นไหม แต่ตอบไม่ได้เต็มที่ว่า
“ยากจริงไหม”, “ตัวลวงดีไหม” หรือ “นักเรียนต้องคิดกี่ขั้น”

รอบนี้จึงอ่าน hard questions ที่ผ่านและ failure rows ของทั้งหกวิชาโดยตรง และใช้
semantic quality reviewer เป็น advisory เท่านั้น. งาน benchmark ต่อไปควรรายงาน
สามชั้นแยกกัน:

1. `generated drafts` — model ส่งอะไรมา รวมข้อเกินและข้อที่ไม่ผ่าน
2. `ship-ready QC` — deterministic acceptance ตาม source/contract
3. `semantic review` — groundedness, correctness, distractor quality และ
   difficulty fit; ถ้าจะใช้เป็นหลักฐานอิสระต้องเปลี่ยน reviewer/model หรือ human
   sample ไม่ใช่ให้ generator ตัดสินตัวเอง

## Dynamic workflow ที่ใช้ต่อ

```text
extract → extraction diagnostics → content chunks
  → graph/outline compile → lesson + coverage slots
  → preflight contract → slot-local evidence packet
  → one set-generation call → deterministic QC
  → repair เฉพาะ missing/failed slots → report drafts/QC/semantic แยกกัน
```

ลำดับนี้เป็น subject-agnostic; prompt หลักไม่ผูก biology/physics แล้ว. ข้อยกเว้น
คือ legacy benchmark ที่ไม่มี lesson hint ซึ่งยังคงมี physics fixture เพื่อกัน
regression ของชุดเก่า ไม่ใช่ข้อจำกัดของ runtime prompt ใหม่

## สิ่งที่ยังไม่ควรทำ

- ไม่เพิ่ม per-question semantic judge หรือ planner ใหม่ตอนนี้: เพิ่ม call โดยตรง
  และไม่ได้แก้ root cause ที่เห็นใน failure rows
- ไม่ขยับ DeepSeek compile จาก 4 เป็น 8 chunks: A/B เดิมลด atom/chunk coverage
  แม้ JSON ยัง valid; 4 chunks / 8,000 runes ยังเป็น default
- ไม่ผ่อน quote/operation/stem gate เพื่อดันตัวเลข: failures ล่าสุดแสดงว่า gate
  กำลังกันข้อที่ไม่พร้อมใช้จริง

รายละเอียด research และ source links อยู่ที่
`docs/research/exam-quality-research-2026-08.md`

## ผลวัดล่าสุดที่ต้องถือเป็น baseline

จาก artifact ฟิสิกส์ OpenStax หน้า 140–220:

- 3 benchmark cases × 5 ข้อ: deterministic QC ผ่าน 15/15
- quality reviewer รายงาน 20/20 ทุก case แต่เป็น model เดียวกับ generator จึงยัง
  ไม่ใช่หลักฐานคุณภาพอิสระ
- graph มี 193 concepts, 323 edges, 670 evidence atoms จาก 186 chunks
- ข้อที่ผ่าน 15 ข้อใช้ atom ไม่ซ้ำกันเพียง 9 atom; ตัวเลขนี้ไม่ใช่ปัญหาในตัวเอง
  เพราะ sample เล็ก แต่ชี้ว่า writer เห็น atom pool ที่ไม่ได้ใช้จำนวนมาก
- บันทึก warm run เดิม: 50 provider calls, input 252,528 tokens และ output
  20,532 tokens; มี bounded repair 4 ครั้ง

## ประเมิน graph compile ใหม่

### สิ่งที่ทำได้ดี

- `NormalizeEvidenceGraph` ตรวจว่า atom มี chunk จริงและ quote อยู่ใน chunk จริง
- atom มี claim, relation, conditions, variables และ question forms ทำให้
  contract เลือก skill ได้ดีกว่าใช้ topic title อย่างเดียว
- graph ช่วยคุม provenance และความหลากหลายของ slot ได้จริง

### จุดที่ยังไม่ถึงเป้าหมาย

- compile ปกติรับเฉพาะ content chunks; โหมด `KeepAllTopics` ยังรักษา behavior เดิม
  สำหรับ regression และกรณีที่ต้องการ audit ทุกหน้า
- edges ปัจจุบันเป็น `co_occurs`/`follows` จาก concept map ไม่ใช่ semantic
  relation ระหว่าง atoms
- `CoverageSlot` มี support atoms สำหรับ hard application และ `Operation` ถูก gate
  ให้ตรงกับ slot; graph ยังไม่ได้เป็น semantic multi-hop graph เต็มรูปแบบ
- `LessonContext` เดิน graph 2 hops แล้วขยายกว้างมากได้เหมือนเดิม แต่ก่อน set
  generation จะถูกตัดเป็น slot-local packet ไม่เกิน 10 chunks/18,000 runes
- ผลใหม่คือ graph ยังทำหน้าที่ routing/provenance เป็นหลัก ส่วน raw chunk ทำหน้าที่
  เป็นหลักฐานสำหรับ quote; การคิดข้ามบทที่ลึกกว่านี้ยังเป็นงานถัดไป

## DeepSeek graph-compile batching audit

ค่าปัจจุบันอยู่ที่ `max 4 chunks` หรือ `8,000 runes` ต่อ compile request และ
`max_tokens=4,096`.

- ใน DeepSeek adapter ค่า `NumCtx=12,288` ของ compile ไม่ได้ถูกส่งไป API; adapter
  ส่งจริงเฉพาะ `max_tokens`, temperature และ top-p ดังนั้น batching ตอนนี้เป็น
  hard-coded safety limit ไม่ใช่ limit ที่ปรับตาม DeepSeek context window
- artifact ฟิสิกส์มี 205 chunks / 196,478 runes. แผนปัจจุบันทำ 52 batches และ
  batch ที่ใหญ่สุดมีเพียง 4,784 runes: ตัวชนเพดานคือ 4 chunks ไม่ใช่ 8,000 runes
- สมมติฐานเชิงคำนวณจากข้อมูลเดียวกัน: 6/12,000 จะอยู่ราว 35 batches,
  8/16,000 ราว 26 batches และ 12/24,000 ราว 18 batches
- `max_tokens=4,096` ทำให้ 8 chunks เป็น candidate แรกที่สมเหตุผล; 12 chunks
  อาจประหยัด calls กว่าแต่เสี่ยง JSON/atom output ถูกตัด โดยยังไม่มี A/B จริงรองรับ
- สรุปก่อนทดลอง: batching ปัจจุบันเสถียรและ conservative แต่ยังไม่ได้ tune
  สำหรับ DeepSeek และยังพูดไม่ได้ว่า “ดีที่สุด” จนกว่าจะวัด output truncation,
  retry, atom recall, input/output tokens และ grounded quality แบบ source เดิม

การทดลอง batching ต้องเริ่ม 4 vs 8 chunks โดยคง source/provider/model/pages/budget
เหมือนเดิม. ยังไม่ขยับเป็น 12 จนกว่า 8 จะผ่าน JSON และ atom-recall parity.

### ผล A/B จริงกับ DeepSeek

ทดสอบ `CompileEvidence` จาก graph/chunks ชุดเดียวกันของ OpenStax หน้า 140–220
ด้วย `deepseek-chat` และ `max_tokens=4096`:

| profile | calls | input tokens | output tokens | atoms | chunks ที่มี atom | เวลา |
|---|---:|---:|---:|---:|---:|---:|
| 4 chunks / 8k | 53 | 162,010 | 103,339 | 687 | 191 | 554.7s |
| 8 chunks / 16k | 33 | 131,073 | 106,930 | 413 | 139 | 558.0s |

ทั้งสองรอบ JSON ผ่าน ไม่มี error/retry ที่ทำให้รันล้ม และ atom ที่ผ่าน normalize
ไม่มี claim หรือ quote ว่าง แต่ profile 8 สูญเสีย atom 39.9% และลด chunk coverage
จาก 191 เหลือ 139 ขณะที่ output tokens กลับเพิ่ม 3.5% และเวลาแทบไม่ลด
จึงไม่ใช่การเพิ่มคุณภาพต่อ token; มีแนวโน้มว่า output ต่อ batch ใหญ่ถูกย่อ/รวม
claim มากเกินไป แม้ syntax จะยังถูกต้อง

**Verdict:** ไม่ promote 8 chunks เป็น default และไม่ลอง 12 ตอนนี้. คืน production
ให้ใช้ 4 chunks / 8k ต่อไป แล้วไปทำ P0 slot-local evidence packet และ deterministic
context ranking ซึ่งมีโอกาสลด token ของ writer โดยไม่ลด provenance coverage

## ลำดับปรับคุณภาพ — สถานะ

### P0 — ทำแล้ว โดยไม่เพิ่ม LLM call

1. **Slot-local evidence packet** — ใน set prompt ส่ง atom/quote/chunk ที่ slot
   เลือกเป็นหลัก และส่ง neighbor เฉพาะที่เกี่ยวกับ relation ของ slot แทนการส่ง
   atoms ทั้งหมดใน context. ลด input tokens และลด evidence drift พร้อมกัน
2. **Deterministic context ranking** — ให้ source chunk ของ slot และ chunks ที่
   atom อ้างถึงมาก่อน document order; cap 24 เป็นเพดานจริง ไม่ใช่ตัวเลือกข้อมูล
   แบบสุ่มตามตำแหน่งในหนังสือ
3. **Compile เฉพาะ content chunks** — ไม่ส่ง chunks ที่ถูก map เป็น apparatus หรือ
   page furniture เข้า evidence compiler. คุณภาพเท่าเดิมหรือนิ่งขึ้นและลด input
   ของ cold pass
4. **บังคับ contract ให้ลึกขึ้น** — gate ตรวจ `Operation` ให้ตรง atom/slot และ
   เพิ่ม heuristic ราคาถูกสำหรับ application/hard เช่น ต้องมี changed condition,
   input/output หรือ operation ที่แตกต่างจริง ไม่รับข้อ recall ที่ติด label hard
5. **ผูก calculator กับ slot** — ส่งเฉพาะ quote/variables ของ calculation slot
   แทนการรวมข้อความของทุก application/calculation chunk เป็นก้อนเดียว ลดค่า
   facts ที่ไม่เกี่ยวและลด prompt leakage โดยไม่เพิ่ม call

### P1 — รอบถัดไป

6. ปรับ `QualityPrompt` ให้ reviewer ตรวจ “claimed key” จาก stem/choices/source
   อย่างอิสระมากขึ้น และให้ `setCandidateScore` ใช้ semantic score เมื่อมีหลาย
   candidate; ไม่เพิ่ม reviewer call ใหม่
7. ยังไม่เพิ่ม per-question judge หรือ planner ใหม่ จนกว่า P0 จะวัดแล้ว เพราะเป็น
   call เพิ่มและเคยพิสูจน์แล้วว่า model ตรวจตัวเองไม่ได้ดีพอ

## วิธีทดลองรอบถัดไป

ใช้ source/provider/pages/budget เดิมและ `set-candidates=1` ก่อน เพื่อแยกผล prompt:

1. รัน baseline เดิมเทียบกับ current packet/ranking บน source เดียวกัน
2. วัด draft waste แยก first attempt กับ bounded repair
3. อ่าน hard/easy/medium ตัวอย่างจริงข้ามวิชา ไม่ดู gate summary อย่างเดียว
4. เพิ่ม independent semantic/human calibration เฉพาะเมื่อ failure ยังเป็น
   genuine difficulty หรือ distractor quality ไม่ใช่ provenance/contract

เก็บทุกครั้ง: provider calls, input/output tokens, accepted rate, quote/coverage
failures, จำนวน application ที่เป็น changed situation จริง และจำนวน hard ที่มี
linked steps อย่างน้อยสองขั้น. ค่อยเปิด `set-candidates=3` หลังคุณภาพต่อ candidate
ดีขึ้นจริง เพราะ 3 candidates คูณค่า generation โดยตรง

## จุดเริ่มแก้ที่ควรเปิดก่อน

- graph compiler: `backend/prototype-exam-quality/llm/generation/generator.go:156`
- graph/context/contract: `backend/prototype-exam-quality/examgen/evidence/evidence.go:58`
- set prompt: `backend/prototype-exam-quality/examgen/generation/prompt.go:349`
- set generation/review: `backend/prototype-exam-quality/examgen/generation/pipeline.go:307`
- token accounting: `backend/prototype-exam-quality/llm/core/stats.go`

เป้าหมายรอบนี้ไม่ใช่ดัน gate pass rate ให้สูงขึ้น แต่ทำให้ข้อที่ผ่านมี
source-specific reasoning มากขึ้นต่อ token และวัดเทียบ NotebookLM ด้วย sample
ที่อ่านเนื้อหาจริงได้
