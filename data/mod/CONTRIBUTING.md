# Contributing to YargAccess

Thanks for wanting to help. This guide covers what you need to run the mod and the launcher, how
to build both from source, and how to send a change.

The project lives in two repositories: the mod in `MichaelJohann1/YargAccess` and the launcher in
`MichaelJohann1/YargAccessLauncher`.

## How changes are accepted

Every change has to come as a pull request from your own fork:

1. Fork the repository on GitHub (the mod or the launcher, whichever you are changing).
2. Make your change in your fork, on a branch of its own.
3. Open a pull request from that branch to this repository.

A change sent any other way will not be reviewed or accepted, whether that is a patch file, a zip,
code pasted into Discord or an email. Both projects are under the GNU General Public License
version 3, and a fork plus a pull request keeps every change public, under that licence, with its
author and its history attached.

In the pull request, say what the change does, why, and how you tested it. Say which screen reader
you used and what you heard: most of this project is about what gets spoken and played, and a
change that builds cleanly can still sound wrong. If you aren't sure an idea fits, ask in the
yarg-support channel on [the Discord server](https://discord.gg/V8tvAN84Mr) before you
start.

## What you need

To run it:

- Windows 10 or 11, 64-bit.
- A screen reader. NVDA works best. Other screen readers, such as JAWS, are reached
  through UniversalSpeech, which falls back to Windows' own SAPI voices when none is running.
- YARG with MelonLoader, the mod and the speech files. The launcher sets all of that up.

To build the mod:

- The .NET SDK (the project targets .NET Framework 4.7.2, `net472`).
- A YARG install that already has MelonLoader. The mod builds against the game's own DLLs, so
  install YARG with the launcher first.

To build the launcher:

- Go 1.26 or newer, on your `PATH`.
- PowerShell, for the build scripts.

## Building the mod

From the mod repository:

```
dotnet build src\YargAccess.csproj -c Release -p:GameDir="C:\path\to\YARG\installation"
```

Leave out `GameDir` and it uses the launcher's default Stable install. The build writes
`bin\Release\YargAccess.dll`. Copy that into the game's `Mods` folder and start YARG.

Two rules for mod code:

- Every Harmony patch class needs a `Prepare` guard (`Patches\PatchGuard.cs`). Then if a YARG
  update renames a method, that one patch is skipped and the rest still load.
- Every spoken line goes through `Speech.Say`, and caught errors go through `ModLog.Warn`, so they
  end up in the log.

## Building the launcher

From the `launcher` folder:

```
go run ./cmd/YargAccessLauncher
go test ./...
```

`tools\build.ps1` runs the tests and builds `dist\YargAccessLauncher\YargAccessLauncher.exe`, its
`README.html` and the release zip. The version comes from `internal\app\app.go`.

The launcher shows the mod's read me, changelog, this guide and the licences on its Documents tab.
After any of them changes in the mod, run `tools\sync-mod-docs.ps1` and build again.

## Scripts

| Script | What it does |
| --- | --- |
| `tools\package-mod.ps1 -Version <version>` | Builds the mod release zip (`Mods\YargAccess.dll`, licences and the speech DLLs) that the launcher installs. Pass the release's version number. Build the mod first. |
| `tools\restore-install.ps1` | Puts MelonLoader, the `Mods` and `UserLibs` folders, `Loader.cfg` and the speech DLLs back into a YARG install after a game update wiped them. `-Install` is the game folder, `-Donor` a folder with a working MelonLoader 0.7. Players should use the launcher's Repair instead. |
| `launcher\tools\build.ps1` | Tests and builds the launcher and its release zip. |
| `launcher\tools\sync-mod-docs.ps1` | Copies the mod's read me, changelog, this guide and the licences into the launcher. |

## Releases

Releases are made by the maintainer, not in pull requests. Both projects ship as GitHub releases
tagged with the version number, with a `v` in front, and the launcher installs the newest one of each.

## Licence

By sending a pull request you agree that your change is released under the GNU General Public
License version 3 or later, with the same extra permission as the rest of the project. See
`LICENSE` and `THIRD-PARTY-LICENSES.md`.
