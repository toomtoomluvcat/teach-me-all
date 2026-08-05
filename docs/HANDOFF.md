# Handoff — exam-quality prototype

อัปเดต 2026-08-06 บน branch `prototype/exam-quality` ที่ `48667e4`.
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

- compile รับ `chunks` ทั้งหมด แม้ map จะคัด apparatus/page furniture ออกแล้ว
  จึงเสีย input tokens กับเนื้อหาที่ไม่มีสิทธิ์สร้างข้อสอบ
- edges ปัจจุบันเป็น `co_occurs`/`follows` จาก concept map ไม่ใช่ semantic
  relation ระหว่าง atoms
- `CoverageSlot` ชี้ atom เดียวต่อข้อ และ `Operation` ยังไม่ได้ถูก gate ตรวจ
  ดังนั้น graph ยังไม่ได้บังคับ reasoning path ที่ประกอบจาก 2+ atoms
- `LessonContext` เดิน graph 2 hops แล้วขยายกว้างมาก ก่อนตัดเหลือไม่เกิน 24
  chunks ตาม document order: ใน artifact เดียวกัน L01 ขยาย 30→90 chunks,
  L03 50→106 และ L07 12→68 ก่อน cap
- ผลคือ graph มี leverage สูงด้าน provenance แต่ leverage ต่ำด้านการคิดข้ามบท;
  context ที่กว้างยังทำให้ writer มีหลักฐานอื่นให้ drift ไปหาได้

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

## ลำดับปรับคุณภาพ โดยเพิ่ม token น้อยที่สุด

### P0 — ไม่เพิ่ม LLM call

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

### P1 — ใช้ call เดิมให้คุ้มขึ้น

6. ปรับ `QualityPrompt` ให้ reviewer ตรวจ “claimed key” จาก stem/choices/source
   อย่างอิสระมากขึ้น และให้ `setCandidateScore` ใช้ semantic score เมื่อมีหลาย
   candidate; ไม่เพิ่ม reviewer call ใหม่
7. ยังไม่เพิ่ม per-question judge หรือ planner ใหม่ จนกว่า P0 จะวัดแล้ว เพราะเป็น
   call เพิ่มและเคยพิสูจน์แล้วว่า model ตรวจตัวเองไม่ได้ดีพอ

## วิธีทดลองรอบถัดไป

ใช้ source/provider/pages/budget เดิมและ `set-candidates=1` ก่อน เพื่อแยกผล prompt:

1. baseline ปัจจุบัน
2. slot-local evidence packet
3. slot-local + deterministic context ranking
4. เพิ่ม operation/depth QC

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
