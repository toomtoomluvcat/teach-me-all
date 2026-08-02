"""Small, embedded Docling runner used by the Go prototype.

The Go process owns discovery, caching, and failure policy. This file only
does the Python-side conversion and writes a single JSON result to stdout.
Everything diagnostic goes to stderr so the result is safe to parse.
"""

from __future__ import annotations

import argparse
import importlib.util
import json
import logging
import mimetypes
import re
import shutil
import sys
from contextlib import redirect_stdout
from pathlib import Path


LOG = logging.getLogger("protoexam.docling")
PAGE_BREAK = "<!-- protoexam-page-break -->"


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="run the embedded Docling extractor")
    parser.add_argument("--check", action="store_true")
    parser.add_argument("--pdf")
    parser.add_argument("--output-dir")
    parser.add_argument("--from-page", type=int, default=0)
    parser.add_argument("--to-page", type=int, default=0)
    parser.add_argument("--requested-mode", default="docling")
    parser.add_argument("--resolved-mode", default="docling")
    parser.add_argument("--ocr-engine", choices=("auto", "rapidocr", "easyocr"), default="auto")
    parser.add_argument("--ocr-lang", default="auto")
    parser.add_argument("--ocr-full-page", action="store_true")
    args = parser.parse_args()
    if not args.check and (not args.pdf or not args.output_dir):
        parser.error("--pdf and --output-dir are required unless --check is used")
    return args


def check_runtime() -> dict:
    try:
        import docling  # noqa: F401
        from docling.backend.pypdfium2_backend import PyPdfiumDocumentBackend  # noqa: F401
        from docling.document_converter import DocumentConverter, PdfFormatOption  # noqa: F401
        from docling.pipeline.standard_pdf_pipeline import StandardPdfPipeline  # noqa: F401
        return {"available": True}
    except Exception as exc:  # pragma: no cover - exercised by the Go fake runner
        LOG.error("Docling runtime import failed: %s", exc)
        return {"available": False, "error": str(exc)}


def parse_langs(value: str) -> list[str]:
    if not value or value == "auto":
        return []
    return [part.strip().lower() for part in value.split(",") if part.strip()]


def choose_ocr(engine: str, lang_value: str):
    from docling.datamodel.pipeline_options import EasyOcrOptions, RapidOcrOptions

    requested = parse_langs(lang_value)
    has_easyocr = importlib.util.find_spec("easyocr") is not None
    has_thai = "th" in requested

    if engine == "auto":
        if has_thai and has_easyocr:
            engine = "easyocr"
        else:
            engine = "rapidocr"
            if has_thai and not has_easyocr:
                LOG.warning("EasyOCR is not installed; using RapidOCR without Thai recognition")

    if engine == "easyocr":
        if not has_easyocr:
            raise RuntimeError(
                "EasyOCR is not installed; install easyocr or use --docling-ocr-engine rapidocr"
            )
        langs = requested or ["th", "en"]
        return engine, langs, EasyOcrOptions(lang=langs, force_full_page_ocr=False)

    # RapidOCR's bundled model is strongest for Latin/English in this runtime.
    langs = requested or ["en"]
    if "th" in langs:
        LOG.warning("RapidOCR does not provide the requested Thai model; using English OCR")
        langs = [lang for lang in langs if lang != "th"] or ["en"]
    return engine, langs, RapidOcrOptions(lang=langs, force_full_page_ocr=False)


def portable_markdown(text: str, root: Path, page_file: bool = False) -> str:
    text = text.replace("\\", "/")
    root_text = root.as_posix().rstrip("/") + "/"
    text = text.replace(root_text, "")
    # Docling normally emits relative refs when reference_path is supplied;
    # this also repairs a file:// URI if a future version changes that detail.
    text = re.sub(r"(?:file://)?[A-Za-z]:/[^) ]*/(assets/[^) ]+)", r"\1", text)
    if page_file:
        text = re.sub(r"\((assets/[^)]+)\)", r"(../\1)", text)
    return text


def combine_page_markdown(page_markdown: list[str]) -> str:
    """Build one document while retaining one section per physical PDF page."""
    sections = [
        re.sub(r"\(\.\./(assets/[^)]+)\)", r"(\1)", markdown).strip()
        for markdown in page_markdown
    ]
    return f"\n\n{PAGE_BREAK}\n\n".join(sections)


def align_page_sections(
    page_numbers: list[int], sections: list[str], pages_with_content: set[int]
) -> list[str]:
    """Map serializer sections back to physical pages, including empty ones."""
    expected = sum(page_no in pages_with_content for page_no in page_numbers)
    if len(sections) != expected:
        raise RuntimeError(
            f"Docling Markdown contained {len(sections)} content sections for "
            f"{expected} non-empty pages ({len(page_numbers)} physical pages)"
        )
    section_iter = iter(sections)
    return [
        next(section_iter) if page_no in pages_with_content else ""
        for page_no in page_numbers
    ]


def write_text(path: Path, text: str) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(text.rstrip() + "\n", encoding="utf-8")


def build_manifest(args, pages: list[dict], assets: list[dict], source_sha256: str | None):
    by_page: dict[int, list[dict]] = {}
    for asset in assets:
        by_page.setdefault(asset["page"], []).append(asset)
    for page in pages:
        page["assets"] = by_page.get(page["number"], [])
    return {
        "schema_version": 1,
        "source_pdf": args.pdf,
        "source_sha256": source_sha256,
        "requested_mode": args.requested_mode,
        "resolved_mode": args.resolved_mode,
        "extraction_mode": args.resolved_mode,
        "page_range": {"from": args.from_page, "to": args.to_page},
        "pages": [
            {
                "number": page["number"],
                "markdown_path": page["markdown_path"],
                "text_path": page["text_path"],
                "assets": page["assets"],
            }
            for page in pages
        ],
        "assets": assets,
        "warnings": [],
    }


def sha256_file(path: Path) -> str | None:
    import hashlib

    try:
        digest = hashlib.sha256()
        with path.open("rb") as stream:
            for block in iter(lambda: stream.read(1024 * 1024), b""):
                digest.update(block)
        return digest.hexdigest()
    except OSError as exc:
        LOG.warning("source SHA-256 unavailable: %s", exc)
        return None


def convert(args) -> dict:
    from docling.backend.pypdfium2_backend import PyPdfiumDocumentBackend
    from docling.datamodel.base_models import InputFormat
    from docling.datamodel.document import ConversionStatus
    from docling.datamodel.pipeline_options import PdfPipelineOptions
    from docling.datamodel.settings import settings
    from docling.document_converter import DocumentConverter, PdfFormatOption
    from docling.pipeline.standard_pdf_pipeline import StandardPdfPipeline
    from docling_core.types.doc import ImageRefMode

    root = Path(args.output_dir).resolve()
    assets_dir = root / "assets"
    pages_dir = root / "pages"
    root.mkdir(parents=True, exist_ok=True)

    # output-dir is a managed extraction bundle. A fresh conversion must not
    # inherit figures or page files from an older engine/range, otherwise the
    # new manifest can claim stale images that are not referenced by this PDF.
    for directory in (assets_dir, pages_dir):
        if directory.exists():
            shutil.rmtree(directory)
    for filename in ("document.md", "document.txt", "docling.json", "manifest.json"):
        (root / filename).unlink(missing_ok=True)
    assets_dir.mkdir(parents=True, exist_ok=True)
    pages_dir.mkdir(parents=True, exist_ok=True)

    settings.perf.page_batch_size = 1
    engine, langs, ocr_options = choose_ocr(args.ocr_engine, args.ocr_lang)
    if args.ocr_full_page:
        ocr_options.force_full_page_ocr = True
    LOG.info("Docling extraction: engine=%s lang=%s page_batch_size=1", engine, ",".join(langs))
    print(f"stage extraction/docling engine={engine} lang={','.join(langs)}", file=sys.stderr)

    pipeline_options = PdfPipelineOptions(
        do_table_structure=True,
        do_ocr=True,
        ocr_options=ocr_options,
        generate_picture_images=True,
        generate_page_images=False,
        images_scale=2.0,
    )
    format_option = PdfFormatOption(
        pipeline_options=pipeline_options,
        backend=PyPdfiumDocumentBackend,
        pipeline_cls=StandardPdfPipeline,
    )
    converter = DocumentConverter(
        allowed_formats=[InputFormat.PDF],
        format_options={InputFormat.PDF: format_option},
    )
    first = args.from_page if args.from_page > 0 else 1
    last = args.to_page if args.to_page > 0 else sys.maxsize
    result = converter.convert(args.pdf, page_range=(first, last))
    if result.status not in (ConversionStatus.SUCCESS, ConversionStatus.PARTIAL_SUCCESS):
        raise RuntimeError(f"Docling conversion status: {result.status}")
    document = result.document
    page_numbers = sorted(document.pages)

    # Save referenced pictures once. Docling's page-break placeholder marks
    # transitions between serialized content, so empty physical pages do not
    # produce a section and must be aligned explicitly below.
    document.save_as_markdown(
        root / "document.md",
        artifacts_dir=assets_dir,
        image_mode=ImageRefMode.REFERENCED,
        page_break_placeholder=PAGE_BREAK,
    )
    serialized = portable_markdown(
        (root / "document.md").read_text(encoding="utf-8"), root
    )
    sections = [] if not serialized.strip() else serialized.split(PAGE_BREAK)
    pages_with_content = {
        page_no
        for page_no in page_numbers
        if document.export_to_markdown(
            page_no=page_no, image_mode=ImageRefMode.PLACEHOLDER
        ).strip()
    }
    page_sections = align_page_sections(page_numbers, sections, pages_with_content)

    page_records: list[dict] = []
    page_markdown: list[str] = []
    combined_text: list[str] = []
    for page_no, section in zip(page_numbers, page_sections):
        markdown_path = f"pages/page-{page_no:04d}.md"
        text_path = f"pages/page-{page_no:04d}.txt"
        markdown = portable_markdown(section, root, page_file=True)
        write_text(root / markdown_path, markdown)
        plain = document.export_to_text(page_no=page_no, traverse_pictures=True)
        write_text(root / text_path, plain)
        page_records.append(
            {
                "number": page_no,
                "markdown": markdown,
                "plain_text": plain.strip(),
                "markdown_path": markdown_path,
                "text_path": text_path,
            }
        )
        page_markdown.append(markdown)
        combined_text.append(f"Page {page_no}\n\n{plain.strip()}\n")

    write_text(root / "document.md", combine_page_markdown(page_markdown))
    write_text(root / "document.txt", "\n".join(combined_text))

    # Structural JSON preserves layout/provenance; images remain referenced in
    # the Markdown bundle rather than being embedded into this file.
    document.save_as_json(
        root / "docling.json",
        image_mode=ImageRefMode.PLACEHOLDER,
    )

    assets: list[dict] = []
    for path in sorted(assets_dir.rglob("*")):
        if not path.is_file():
            continue
        relative = path.relative_to(root).as_posix()
        if relative.startswith("assets/page_"):
            # Defensive guard against a backend violating generate_page_images=False.
            path.unlink(missing_ok=True)
            continue
        page_no = 0
        for page in page_records:
            if path.name in page["markdown"]:
                page_no = page["number"]
                break
        asset = {
            "page": page_no,
            "path": relative,
            "kind": "figure",
            "mime": mimetypes.guess_type(path.name)[0] or "application/octet-stream",
            "size": path.stat().st_size,
        }
        assets.append(asset)

    manifest = build_manifest(args, page_records, assets, sha256_file(Path(args.pdf)))
    write_text(root / "manifest.json", json.dumps(manifest, ensure_ascii=False, indent=2))
    result = {
        "resolved_ocr_engine": engine,
        "resolved_ocr_lang": langs,
        "pages": page_records,
        "assets": assets,
        "warnings": [],
    }
    print(f"stage extraction/docling pages={len(page_records)} assets={len(assets)}", file=sys.stderr)
    return result


def main() -> int:
    logging.basicConfig(stream=sys.stderr, level=logging.INFO)
    args = parse_args()
    try:
        # Some optional backends print during import. Keep stdout reserved for
        # the machine-readable final JSON object.
        with redirect_stdout(sys.stderr):
            payload = check_runtime() if args.check else convert(args)
        print(json.dumps(payload, ensure_ascii=False, separators=(",", ":")))
        return 0 if payload.get("available", True) else 1
    except Exception:
        LOG.exception("Docling extraction failed")
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
