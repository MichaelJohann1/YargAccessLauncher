# YARG Access launcher licences

The YARG Access launcher is free software: you can redistribute it and/or modify it under the terms
of the GNU General Public License as published by the Free Software Foundation, either version 3 of
the License, or (at your option) any later version. See `LICENSE` for the full text.

It is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY; without even the
implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU General Public
License for more details.

Copyright (C) 2026 Michael Johann

## Additional permission under GNU GPL version 3 section 7

The chart preview plays sound through the BASS audio library (`bass.dll`) of the user's YARG
install, which is not under the GPL. If you modify this program, or any covered work, by linking or
combining it with BASS (or a modified version of it), the licensors of this program grant you
additional permission to convey the resulting work. Corresponding Source for a non-source form of
such a combination shall not include the source code for the parts of BASS used as well as that of
the covered work.

## Included in the launcher download

The built launcher is one Go program with `prism.dll` beside it in `_internal`. It carries these libraries:

| Component | Licence | Where |
|---|---|---|
| Go standard library and runtime | BSD 3-Clause | https://go.dev |
| golang.org/x/crypto, x/sys, x/text | BSD 3-Clause | https://cs.opensource.google/go/x |
| Prism (`prism.dll`), speech output | Mozilla Public License 2.0 | https://github.com/ethindp/prism |
| Walk and win, the window and its controls | BSD 3-Clause | https://github.com/lxn/walk |
| go-webview2 and go-winloader, the document viewer | MIT and ISC | https://github.com/jchv/go-webview2 |
| Microsoft WebView2 loader (inside go-webview2) | BSD 3-Clause (Microsoft) | https://developer.microsoft.com/microsoft-edge/webview2 |
| sevenzip, plumbing and windows, by Matt Dainty | BSD 3-Clause | https://github.com/bodgit/sevenzip |
| compress, by Klaus Post | BSD 3-Clause | https://github.com/klauspost/compress |
| xz, by Ulrich Kunitz | BSD 3-Clause | https://github.com/ulikunitz/xz |
| lz4, by Pierre Curto | BSD 3-Clause | https://github.com/pierrec/lz4 |
| ppmd, by Clayton Stangeland | MIT | https://github.com/stangelandcl/ppmd |
| Brotli (Go port) | MIT | https://github.com/andybalholm/brotli |
| golang-lru, by HashiCorp | Mozilla Public License 2.0 | https://github.com/hashicorp/golang-lru |
| afero | Apache License 2.0 | https://github.com/spf13/afero |
| go4.org | Apache License 2.0 | https://github.com/go4org/go4 |
| govaluate | MIT | https://github.com/Knetic/govaluate |

The MPL libraries are used unchanged; their source is at the links above.

## Downloaded by the launcher from their own sources, not included in it

The launcher fetches these from their makers' servers when you install or download them. Their
licence texts come with them only where their makers include one: MelonLoader puts its licence and
notice in `MelonLoader\Documentation`, the mod puts its own in `Mods\YargAccess`, and YARG's
builds carry none (YARG's licence is on its GitHub page).

| Component | Licence | From |
|---|---|---|
| YARG, by YARC | GNU LGPL v3.0 or later | releases.yarg.in |
| YargAccess, the accessibility mod | GNU GPL v3.0 or later; its own notices go to `Mods\YargAccess` in the game folder | the mod's GitHub releases |
| MelonLoader, by LavaGang | Apache License 2.0 | https://github.com/LavaGang/MelonLoader |
| Official setlists, YARN setlists, venues | their makers' terms | releases.yarg.in |
| Charts from Chorus Encore | each charter's terms | https://www.enchor.us |

YARG and its name and logos belong to their owners. The YARG Access launcher is an independent
launcher for YARG with its accessibility mod and is not made or endorsed by YARC.
