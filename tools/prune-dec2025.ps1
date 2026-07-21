<#
.SYNOPSIS
    Prunes the dec2025 Schedule P reference data down to the gr codes listed
    in data/reference/gr-code-list.md.

.DESCRIPTION
    gr-code-list.md holds one "<lob>: <grcode>" entry per line, where <lob> is
    one of the six Schedule P lines of business. The keep-list is scoped per
    line of business: a gr code kept for one LOB is not automatically kept for
    another. For each Schedule P line-of-business subdirectory this script keeps
    only the "<grcode>.json" files whose code is listed for that LOB and deletes
    the rest.

    Runs as a dry-run by default (prints what would be deleted). Pass -Apply to
    actually delete. Deleted files are git-tracked, so an accidental run is
    recoverable with `git checkout -- <path>`.

.EXAMPLE
    pwsh tools/prune-dec2025.ps1            # preview only
    pwsh tools/prune-dec2025.ps1 -Apply     # perform the deletion
#>
[CmdletBinding()]
param(
    [switch]$Apply
)

$ErrorActionPreference = 'Stop'

# Repo root is the parent of this script's directory.
$repoRoot = Split-Path -Parent $PSScriptRoot
$listPath = Join-Path $repoRoot 'data/reference/gr-code-list.md'
$refBase  = Join-Path $repoRoot 'data/reference/schedule p'

# Map the list's LOB prefix to its subdirectory name.
$lobToDir = @{
    'comauto'  = 'comauto_pos_98-07'
    'medmal'   = 'medmal_pos_98-07'
    'othliab'  = 'othliab_pos_98-07'
    'ppauto'   = 'ppauto_pos98-07'
    'prodliab' = 'prodliab_pos_98-07'
    'wkcomp'   = 'wkcomp_pos_98-07'
}

if (-not (Test-Path -LiteralPath $listPath)) {
    throw "Keep-list not found: $listPath"
}

# Parse the keep-list into: LOB -> set of "<grcode>.json" filenames to keep.
$keep = @{}
foreach ($dir in $lobToDir.Values) { $keep[$dir] = [System.Collections.Generic.HashSet[string]]::new() }

$lineNo = 0
foreach ($line in Get-Content -LiteralPath $listPath) {
    $lineNo++
    $trimmed = $line.Trim()
    if ($trimmed -eq '') { continue }

    if ($trimmed -notmatch '^\s*([A-Za-z]+)\s*:\s*(\d+)\s*$') {
        Write-Warning "Skipping unparseable line $lineNo`: '$line'"
        continue
    }
    $lob  = $Matches[1].ToLower()
    $code = $Matches[2]

    if (-not $lobToDir.ContainsKey($lob)) {
        Write-Warning "Skipping unknown LOB on line $lineNo`: '$lob'"
        continue
    }
    [void]$keep[$lobToDir[$lob]].Add("$code.json")
}

# Walk each subdirectory and delete files not in that LOB's keep set.
$totalDeleted = 0
$totalKept    = 0

foreach ($dir in ($lobToDir.Values | Sort-Object)) {
    $subPath = Join-Path $refBase $dir
    if (-not (Test-Path -LiteralPath $subPath)) {
        Write-Warning "Schedule P subdirectory missing: $subPath"
        continue
    }

    $keepSet = $keep[$dir]
    $files = Get-ChildItem -LiteralPath $subPath -Filter '*.json' -File

    $deletedHere = 0
    $keptHere    = 0
    foreach ($f in $files) {
        if ($keepSet.Contains($f.Name)) {
            $keptHere++
            continue
        }
        if ($Apply) {
            Remove-Item -LiteralPath $f.FullName -Force
        }
        $deletedHere++
    }

    # Warn if the list names codes that have no file in this LOB.
    $present = [System.Collections.Generic.HashSet[string]]::new()
    foreach ($f in $files) { [void]$present.Add($f.Name) }
    $missing = @($keepSet | Where-Object { -not $present.Contains($_) })
    if ($missing.Count -gt 0) {
        Write-Warning "$dir`: $($missing.Count) listed code(s) have no file: $($missing -join ', ')"
    }

    Write-Host ("{0,-20} keep {1,4}  delete {2,4}" -f $dir, $keptHere, $deletedHere)
    $totalDeleted += $deletedHere
    $totalKept    += $keptHere
}

$mode = if ($Apply) { 'DELETED' } else { 'would delete (dry-run; pass -Apply to execute)' }
Write-Host ""
Write-Host ("Total kept: {0}  |  {1}: {2}" -f $totalKept, $mode, $totalDeleted)
