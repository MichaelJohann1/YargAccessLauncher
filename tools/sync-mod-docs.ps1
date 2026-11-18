# Copies the mod's read me, changelog and licences into the launcher, for its Documents tab.
#
#   tools\sync-mod-docs.ps1 [path to the mod repository]
#
# The mod repository defaults to the folder above this one (the launcher sits inside it). Writes
# data\mod: CHANGELOG.md, LICENSE and THIRD-PARTY-LICENSES.md as they are, and README.md over the
# launcher's own; the build embeds them.
# Run it after any of them changes in the mod, then build.
param([string]$Mod = "$PSScriptRoot\..\..")
$ErrorActionPreference = "Stop"
$out = "$PSScriptRoot\..\data\mod"
New-Item -ItemType Directory -Force $out | Out-Null
foreach ($name in "CHANGELOG.md", "CONTRIBUTING.md", "LICENSE", "THIRD-PARTY-LICENSES.md") {
    Copy-Item (Join-Path $Mod $name) (Join-Path $out $name) -Force
}
# One read me serves both repositories: the mod's is the launcher's too.
Copy-Item (Join-Path $Mod "README.md") "$PSScriptRoot\..\README.md" -Force
Write-Host "Wrote $((Resolve-Path $out).Path)"
