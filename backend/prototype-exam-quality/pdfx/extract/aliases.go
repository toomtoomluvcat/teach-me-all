package extract

import "protoexam/pdfx/bundle"

type PreparedBundle = bundle.PreparedBundle
type PreparedBundlePage = bundle.PreparedBundlePage
type BundleAsset = bundle.BundleAsset

func MarkdownToPlainText(markdown string) string { return bundle.MarkdownToPlainText(markdown) }
