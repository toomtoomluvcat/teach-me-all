// Package pdfx is the extraction facade. Engine-specific extraction and the
// durable bundle writer are kept in separate subpackages so this boundary stays
// small when the prototype moves into the backend.
package pdfx

import (
	"context"

	"protoexam/pdfx/bundle"
	"protoexam/pdfx/extract"
)

type ProgressFunc = extract.ProgressFunc
type AutoOptions = extract.AutoOptions
type AutoResult = extract.AutoResult
type DoclingOptions = extract.DoclingOptions
type DoclingResult = extract.DoclingResult
type DoclingRunner = extract.DoclingRunner
type ExecDoclingRunner = extract.ExecDoclingRunner

type BundleOptions = bundle.BundleOptions
type PreparedBundle = bundle.PreparedBundle
type PreparedBundlePage = bundle.PreparedBundlePage
type BundleAsset = bundle.BundleAsset
type BundleResult = bundle.BundleResult
type BundleManifest = bundle.BundleManifest
type BundlePageRange = bundle.BundlePageRange
type BundlePage = bundle.BundlePage

func ExtractAuto(ctx context.Context, opts AutoOptions) (AutoResult, error) {
	return extract.ExtractAuto(ctx, opts)
}
func ExtractDocling(ctx context.Context, opts DoclingOptions) (DoclingResult, error) {
	return extract.ExtractDocling(ctx, opts)
}
func ResolveDoclingPython(explicit string) (string, error) {
	return extract.ResolveDoclingPython(explicit)
}
func WriteBundle(opts BundleOptions) (BundleResult, error) { return bundle.WriteBundle(opts) }
func MarkdownToPlainText(markdown string) string           { return bundle.MarkdownToPlainText(markdown) }
