# Builds dist\YargAccessLauncher\YargAccessLauncher.exe (with prism.dll in _internal) and the release
# zip dist\YargAccessLauncher-<version>.zip. Needs Go 1.26 or newer on PATH.
$ErrorActionPreference = "Stop"
Set-Location "$PSScriptRoot\.."

$version = (Select-String -Path internal\app\app.go -Pattern 'Version\s*=\s*"([^"]+)"').Matches[0].Groups[1].Value
$out = "dist\YargAccessLauncher"

go test ./...
if ($LASTEXITCODE -ne 0) { throw "Tests failed ($LASTEXITCODE)" }

Push-Location cmd\YargAccessLauncher
go run github.com/tc-hib/go-winres@v0.3.3 make --arch amd64 --product-version $version --file-version $version
$code = $LASTEXITCODE
Pop-Location
if ($code -ne 0) { throw "go-winres failed ($code)" }

if (Test-Path $out) { Remove-Item -Recurse -Force $out }
New-Item -ItemType Directory -Force "$out\_internal" | Out-Null
go build -trimpath -ldflags "-s -w -H windowsgui" -o "$out\YargAccessLauncher.exe" .\cmd\YargAccessLauncher
if ($LASTEXITCODE -ne 0) { throw "go build failed ($LASTEXITCODE)" }
Copy-Item third_party\prism\prism.dll "$out\_internal\prism.dll"
go run .\cmd\readmehtml "$out\README.html"
if ($LASTEXITCODE -ne 0) { throw "README.html failed ($LASTEXITCODE)" }

$zip = "dist\YargAccessLauncher-$version.zip"
if (Test-Path $zip) { Remove-Item -Force $zip }
Compress-Archive -Path $out -DestinationPath $zip
Write-Host "Built $out\YargAccessLauncher.exe and $zip"
