// Separate module on purpose: this is throwaway code and its dependencies must
// not leak into the production go.mod one directory up.
module protoexam

go 1.25.0

require github.com/ledongthuc/pdf v0.0.0-20250511090121-5959a4027728
