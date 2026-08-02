# Test PDFs

Fixtures for `backend/prototype-exam-quality`. The PDFs themselves are
gitignored — this file records what each one is for and how to get it back.

Each was chosen for a specific way PDF extraction fails, not because it was
handy.

| file | edge case it exercises | where it came from |
|---|---|---|
| `arxiv-typhoon2-twocolumn.pdf` | Two-column LaTeX, dense tables, inline maths, dot-leader contents pages. **The Go extractor gets no glyph geometry at all from this one** — every run reports X, W and FontSize of zero — so it is the case that forces the poppler fallback. | `curl -L -o arxiv-typhoon2-twocolumn.pdf https://arxiv.org/pdf/2412.13702` |
| `openstax-college-algebra.pdf` | A real 1000-page textbook with numbered exercises and worked numeric examples. The case for `--force-calc`, and the case for "does pass 1 survive a document this big". 54 MB. | `curl -L -o openstax-college-algebra.pdf https://assets.openstax.org/oscms-prodcms/media/documents/CollegeAlgebra-OP.pdf` (CC BY 4.0) |
| `scanned-textbook.pdf` | **No text layer at all** — three pages that are each a single JPEG, the way a flatbed scanner or a phone photo produces. Forces `--extract=ocr` and proves the "this is a scan" detection fires instead of silently producing nothing. | Generated. The generator is in the session scratchpad (`mkscan`); it draws text with a bitmap font, upscales it, adds scanner grunge, and wraps each page as a JPEG in a hand-written PDF. |

Not in this table but used during development, from the user's own Downloads:

- A Thai course handout (`ใบความรู้ ... กระบวนการคิด`) — Thai text layer, the
  SARA AM extraction bug, and a repeated page header on every page.
- A Thai/English lab manual (`2025-EN812201-DLDLab_1-...`) — mixed scripts plus
  circuit diagrams whose embedded fonts have no character map, so they extract
  as runs of U+FFFD.

## Still missing

- **A real scan of a real Thai book.** `scanned-textbook.pdf` is synthetic and
  English; typhoon-ocr's behaviour on a genuine Thai scan is untested, and Thai
  is the case it was built for.
- **A slide deck exported to PDF.** Very common for lecture notes: many pages,
  little text per page, heavy layout. Nothing here covers it.
- **A password-protected PDF.** `pdf.Open` has no password prompt; the failure
  mode is unverified.
