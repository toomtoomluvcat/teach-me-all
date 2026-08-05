# Test PDFs

Fixtures for `backend/prototype-exam-quality`. The PDFs themselves are
gitignored — this file records what each one is for and how to get it back.

Each was chosen for a specific way PDF extraction fails, not because it was
handy.

| file | edge case it exercises | where it came from |
|---|---|---|
| `arxiv-typhoon2-twocolumn.pdf` | Two-column LaTeX, dense tables, inline maths, and dot-leader contents pages. Exercises Docling reading order and table/layout recovery. | `curl -L -o arxiv-typhoon2-twocolumn.pdf https://arxiv.org/pdf/2412.13702` |
| `openstax-college-algebra.pdf` | A real 1000-page textbook with numbered exercises and worked numeric examples. The case for `--force-calc`, and the case for "does pass 1 survive a document this big". 54 MB. | `curl -L -o openstax-college-algebra.pdf https://assets.openstax.org/oscms-prodcms/media/documents/CollegeAlgebra-OP.pdf` (CC BY 4.0) |
| `scanned-textbook.pdf` | **No text layer at all** — three pages that are each a single JPEG, the way a flatbed scanner or phone photo produces. Exercises Docling's OCR path. | Generated. The generator is in the session scratchpad (`mkscan`); it draws text with a bitmap font, upscales it, adds scanner grunge, and wraps each page as a JPEG in a hand-written PDF. |
| `thai-highschool-biology-ipst.pdf` | Real Thai high-school layout: ม.5 biology, 254 pages, Thai text, tables, photos, and labelled diagrams. Page 60 is the focused regression case for Markdown reading order plus figure-level extraction. | [IPST/SciMath teacher guide, Biology M.5 book 4](https://www.scimath.org/e-books/10301/flippingbook/files/assets/common/downloads/f.pdf), downloaded from the official สสวท. site. |

Not in this table but used during development, from the user's own Downloads:

- A Thai course handout (`ใบความรู้ ... กระบวนการคิด`) — Thai text layer, the
  SARA AM extraction bug, and a repeated page header on every page.
- A Thai/English lab manual (`2025-EN812201-DLDLab_1-...`) — mixed scripts plus
  circuit diagrams whose embedded fonts have no character map, so they extract
  as runs of U+FFFD.

## Still missing

- **A genuine camera/flatbed scan of a Thai book.** The ม.5 source above is a
  digital PDF; a raster-only derivative can test OCR mechanics but not real
  skew, shadows, blur, or paper texture.
- **A slide deck exported to PDF.** Very common for lecture notes: many pages,
  little text per page, heavy layout. Nothing here covers it.
- **A password-protected PDF.** `pdf.Open` has no password prompt; the failure
  mode is unverified.
