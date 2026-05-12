$ErrorActionPreference = "Stop"

$files = git diff --cached --name-only --diff-filter=ACMR
$blocked = @()

foreach ($file in $files) {
    if ($file -match '(^|/)(session)(/|$)' -or $file -match '\.db(-.*)?$') {
        $blocked += $file
        continue
    }

    if (Test-Path -LiteralPath $file -PathType Leaf) {
        $content = Get-Content -Raw -LiteralPath $file
        if ($content -match '\d{8,}@s\.whatsapp\.net') {
            $blocked += $file
        }
    }
}

if ($blocked.Count -gt 0) {
    Write-Error "Blocked runtime state, database files, or real WhatsApp JIDs:`n$($blocked -join "`n")"
    exit 1
}
