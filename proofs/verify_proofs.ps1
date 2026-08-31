# verify_proofs.ps1 - standalone verification of the shpreflight formal proofs
# Direct-compile recipe (plain `lean`, no lake/git/Mathlib needed).
# Re-exports the runtime danger table from Python first, so the proofs are
# always checked against the code that actually ships — they cannot drift.
# Usage: powershell -ExecutionPolicy Bypass -File proofs\verify_proofs.ps1
# Overridable toolchain: $env:LEAN_BIN (default H:\lean4\lean-4.21.0-windows\bin)
$ErrorActionPreference = 'Continue'

$leanBin = if ($env:LEAN_BIN) { $env:LEAN_BIN } else { 'H:\lean4\lean-4.21.0-windows\bin' }
$lean = Join-Path $leanBin 'lean.exe'
if (-not (Test-Path $lean)) { Write-Host "FAIL: lean.exe not found at $leanBin (set LEAN_BIN)"; exit 1 }

$env:LEAN_PATH = "$leanBin\..\lib\lean;."
Set-Location $PSScriptRoot

# 1. Regenerate Patterns.lean from the live Python table (sorted, deterministic).
if (Get-Command python -ErrorAction SilentlyContinue) {
    python export_patterns.py
    if ($LASTEXITCODE -ne 0) { Write-Host 'FAIL: export_patterns.py'; exit 1 }
} else {
    Write-Host 'note: python not found — checking against the committed Patterns.lean'
}

# 2. Compile each proof file; count errors/warnings/sorries.
$totalErr = 0
foreach ($f in @('Patterns.lean', 'Shpreflight.lean')) {
    $out = & $lean ".\$f" -o ".\$($f -replace '\.lean$', '.olean')" 2>&1
    $errs = @($out | Where-Object { $_ -match ': error' })
    $warns = @($out | Where-Object { $_ -match ': warning' })
    $sorries = @($out | Where-Object { $_ -match "uses 'sorry'" })
    '{0}: exit={1} errors={2} warnings={3} sorry={4}' -f $f, $LASTEXITCODE, $errs.Count, $warns.Count, $sorries.Count
    if ($errs) { $errs | Select-Object -First 15 | ForEach-Object { "  $_" } }
    $totalErr += $errs.Count + $sorries.Count
}

# 3. No sorry/admit may appear anywhere in the proof sources.
$sorryLines = @(Select-String -Path '.\Patterns.lean', '.\Shpreflight.lean' -Pattern '\b(sorry|admit|sorryAx)\b' -ErrorAction SilentlyContinue)
if ($sorryLines) {
    $sorryLines | ForEach-Object { "  sorry: $_" }
    $totalErr += $sorryLines.Count
}

# 4. Axiom audit: proofs must not rest on sorryAx.
@'
import Shpreflight
#print axioms reconstruct_lexer
#print axioms all_patterns_alive
#print axioms patternCodes_nodup
'@ | Set-Content '.\_axioms.lean' -Encoding ascii
$ax = & $lean '.\_axioms.lean' 2>&1 | Out-String
Remove-Item '.\_axioms.lean' -ErrorAction SilentlyContinue
if ($ax -match 'sorryAx') {
    Write-Host 'FAIL: a proof depends on sorryAx'
    Write-Host $ax
    $totalErr += 1
} else {
    Write-Host $ax.TrimEnd()
}

if ($totalErr -gt 0) { Write-Host "FAIL: $totalErr problem(s)"; exit $totalErr }
Write-Host 'OK: all proofs compile, zero sorry, clean axiom audit'
exit 0
