param(
    [string]$Python = "python3.12",
    [string]$Venv = ""
)

$ErrorActionPreference = "Stop"
$repoRoot = Resolve-Path -LiteralPath (Join-Path $PSScriptRoot "..\..")
if ([string]::IsNullOrWhiteSpace($Venv)) {
    $Venv = Join-Path $repoRoot ".scratch\docling-venv"
}

$uv = Get-Command uv -ErrorAction Stop
& $uv.Source venv --python $Python $Venv
if ($LASTEXITCODE -ne 0) { throw "uv venv failed" }

$venvPython = Join-Path $Venv "Scripts\python.exe"
& $uv.Source pip install --python $venvPython `
    "docling==2.117.0" `
    "rapidocr==3.9.2" `
    "onnxruntime==1.28.0" `
    "easyocr==1.7.2"
if ($LASTEXITCODE -ne 0) { throw "Docling dependency install failed" }

& $venvPython (Join-Path $PSScriptRoot "pdfx\extract\docling_helper.py") --check
if ($LASTEXITCODE -ne 0) { throw "Docling runtime check failed" }

Write-Host "Docling runtime ready: $venvPython"
