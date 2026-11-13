# Builds build\bin\easyTTY.exe (Windows, no CGO, no Node needed).
#   -SkipTests   skip unit tests
#   -NoResources skip icon + manifest (go-winres is downloaded on first use)
#   -Portable    also build build\easyTTY-portable.zip (exe + marker, data stays next to it)
#   -Version     version shown in the app (default 0.0.0-dev; releases are built by CI)
param([switch]$SkipTests, [switch]$NoResources, [switch]$Portable, [string]$Version = "0.0.0-dev")
$ErrorActionPreference = "Stop"
Set-Location $PSScriptRoot

if (-not $SkipTests) {
    go test ./...
    if ($LASTEXITCODE -ne 0) { throw "Tests fehlgeschlagen" }
}

if (-not $NoResources) {
    if (-not (Test-Path build\icon.png)) { go run ./tools/genicon }
    $num = ($Version -replace '[^0-9.].*$', '') # "0.2.0-dev" -> "0.2.0"
    go run github.com/tc-hib/go-winres@v0.3.3 make --in winres\winres.json `
        --product-version $num --file-version $num
    if ($LASTEXITCODE -ne 0) { throw "go-winres fehlgeschlagen" }
}

go build -tags desktop,production -trimpath -ldflags "-w -s -H windowsgui -X main.appVersion=$Version" -o build\bin\easyTTY.exe .
if ($LASTEXITCODE -ne 0) { throw "Build fehlgeschlagen" }
Get-Item build\bin\easyTTY.exe | Select-Object Name, @{n = "MB"; e = { [math]::Round($_.Length / 1MB, 1) } }

if ($Portable) {
    # The zip is packed from a fresh staging folder; an existing test copy in
    # build\portable\easyTTY only gets the new exe, its easyTTY-data stays.
    $dir = "build\portable-stage\easyTTY"
    if (Test-Path build\portable-stage) { Remove-Item -Recurse -Force build\portable-stage }
    New-Item -ItemType Directory -Force $dir | Out-Null
    Copy-Item build\bin\easyTTY.exe $dir
    Set-Content -Path "$dir\easyTTY.portable" -Encoding utf8 -Value @"
easyTTY portabel: Solange diese Datei neben easyTTY.exe liegt, speichert easyTTY
alles (Sessions, Einstellungen, Hostkeys, Logs, WebView-Cache) im Ordner
easyTTY-data daneben - nichts im Benutzerprofil, keine Installation, keine Adminrechte.
Gespeicherte Passwoerter sind an den Windows-Benutzer gebunden (DPAPI); auf einem
anderen PC oder Konto fragt easyTTY sie einfach neu ab.
Voraussetzung: Microsoft Edge WebView2 (bei Windows 10/11 vorinstalliert).
"@
    $zip = "build\easyTTY-portable.zip"
    if (Test-Path $zip) { Remove-Item $zip }
    Compress-Archive -Path $dir -DestinationPath $zip
    Get-Item $zip | Select-Object Name, @{n = "MB"; e = { [math]::Round($_.Length / 1MB, 1) } }

    $test = "build\portable\easyTTY"
    New-Item -ItemType Directory -Force $test | Out-Null
    $exe = Join-Path $test "easyTTY.exe"
    $running = Get-Process easyTTY -ErrorAction SilentlyContinue | Where-Object { $_.Path -eq (Join-Path $PSScriptRoot $exe) }
    if ($running) { Write-Warning "$exe laeuft gerade - nicht ersetzt (Zip ist trotzdem neu)." }
    else {
        Copy-Item "$dir\easyTTY.exe" $test -Force
        Copy-Item "$dir\easyTTY.portable" $test -Force
    }
    Remove-Item -Recurse -Force build\portable-stage
}
