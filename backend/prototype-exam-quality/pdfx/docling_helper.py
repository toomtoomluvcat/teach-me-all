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
    parser.add_argument("--ocr-mode", choices=("auto", "on", "off"), default="auto")
    parser.add_argument("--formula-mode", choices=("auto", "on", "off"), default="auto")
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


def inspect_native_text(
    pdf: str, first: int, last: int
) -> tuple[int, int, str, dict[int, str]]:
    """Inspect the embedded text layer without running layout or OCR."""
    import pypdfium2 as pdfium

    document = pdfium.PdfDocument(pdf)
    total_pages = len(document)
    start = max(first, 1)
    end = min(last if last > 0 else total_pages, total_pages)
    native_pages = 0
    samples: list[str] = []
    page_text: dict[int, str] = {}
    for page_number in range(start, end + 1):
        text = document[page_number - 1].get_textpage().get_text_range()
        page_text[page_number] = text
        compact = re.sub(r"\s+", "", text)
        if len(compact) >= 40:
            native_pages += 1
        samples.append(text)
    return native_pages, max(0, end - start + 1), "\n".join(samples), page_text


def resolve_ocr(mode: str, native_pages: int, selected_pages: int) -> tuple[bool, str]:
    if mode == "on":
        return True, "on"
    if mode == "off":
        return False, "off"
    # A mixed PDF is safer with OCR enabled: pages without a text layer still
    # need to be readable. A wholly native PDF can use the backend text layer.
    enabled = selected_pages == 0 or native_pages == 0
    return enabled, "on" if enabled else "off"


def resolve_formula(mode: str, native_text: str) -> tuple[bool, str, bool]:
    if mode == "on":
        return True, "on", True
    if mode == "off":
        return False, "off", False
    # Formula glyphs are often absent from the PDF text layer, but the prose
    # around them usually survives. This keeps formula VLM work off for plain
    # prose while enabling it for math/physics-like source automatically.
    signal = re.search(
        r"(?i)\b(equation|formula|mathematically|proportional|variable|symbol)\b|"
        r"\b(write as|defined mathematically|in equation form)\b",
        native_text,
    )
    # Formula VLM loading is intentionally explicit. Auto detection only tells
    # the caller that the document would benefit from the heavier pass.
    return False, "off", signal is not None


def markdown_to_plain(markdown: str) -> str:
    """Flatten page Markdown while preserving LaTeX and meaningful text."""
    markdown = markdown.replace("\r\n", "\n").replace("\r", "\n")
    markdown = re.sub(r"<!--\s*formula-not-decoded\s*-->", "", markdown, flags=re.I)
    markdown = re.sub(r"!\[[^\]]*\]\([^)]*\)", "", markdown)
    markdown = re.sub(r"\[([^\]]+)\]\([^)]*\)", r"\1", markdown)
    lines: list[str] = []
    for line in markdown.splitlines():
        line = re.sub(r"^\s{0,3}#{1,6}\s*", "", line)
        line = re.sub(r"^\s*(?:[-*+]\s+|\d+[.)]\s+)", "", line)
        line = re.sub(r"[ \t]+", " ", line).strip()
        if line and line not in {"|", "---"}:
            lines.append(line)
    return "\n".join(lines).strip()


def native_text_to_plain(text: str) -> str:
    """Use the PDF text layer without preserving its artificial line wraps."""
    text = text.replace("\r", "\n")
    return re.sub(r"\s+", " ", text).strip()


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
    from docling.datamodel.pipeline_options import OcrAutoOptions, PdfPipelineOptions
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
    first = args.from_page if args.from_page > 0 else 1
    last = args.to_page if args.to_page > 0 else 0
    native_pages, selected_pages, native_text, native_page_text = inspect_native_text(
        args.pdf, first, last
    )
    ocr_enabled, resolved_ocr_mode = resolve_ocr(args.ocr_mode, native_pages, selected_pages)
    if not ocr_enabled and native_pages == selected_pages and selected_pages > 0:
        resolved_ocr_mode = "native"
    formula_enabled, resolved_formula_mode, formula_signal = resolve_formula(
        args.formula_mode, native_text
    )
    if ocr_enabled:
        engine, langs, ocr_options = choose_ocr(args.ocr_engine, args.ocr_lang)
        if args.ocr_full_page:
            ocr_options.force_full_page_ocr = True
    else:
        engine, langs, ocr_options = "disabled", [], OcrAutoOptions()
    LOG.info(
        "Docling extraction: native_pages=%d/%d ocr=%s formula=%s engine=%s lang=%s",
        native_pages, selected_pages, resolved_ocr_mode, resolved_formula_mode,
        engine, ",".join(langs),
    )
    print(
        f"stage extraction/docling native={native_pages}/{selected_pages} "
        f"ocr={resolved_ocr_mode} formulas={resolved_formula_mode} "
        f"engine={engine} lang={','.join(langs)}",
        file=sys.stderr,
    )

    pipeline_options = PdfPipelineOptions(
        do_table_structure=True,
        do_ocr=ocr_enabled,
        do_formula_enrichment=formula_enabled,
        force_backend_text=not ocr_enabled and not formula_enabled,
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
    result = converter.convert(args.pdf, page_range=(first, last or sys.maxsize))
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
        if not ocr_enabled and not formula_enabled:
            plain = native_text_to_plain(native_page_text.get(page_no, ""))
        else:
            plain = markdown_to_plain(markdown)
        if not plain:
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

    warnings = [
        f"native text pages: {native_pages}/{selected_pages}",
        f"OCR: {resolved_ocr_mode}; formulas: {resolved_formula_mode}",
    ]
    if formula_signal and not formula_enabled:
        warnings.append("formula-like prose detected; rerun with --docling-formulas on")
    manifest = build_manifest(args, page_records, assets, sha256_file(Path(args.pdf)))
    manifest["warnings"] = warnings
    write_text(root / "manifest.json", json.dumps(manifest, ensure_ascii=False, indent=2))
    result = {
        "resolved_ocr_engine": engine,
        "resolved_ocr_lang": langs,
        "resolved_ocr_mode": resolved_ocr_mode,
        "resolved_formula_mode": resolved_formula_mode,
        "pages": page_records,
        "assets": assets,
        "warnings": warnings,
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
