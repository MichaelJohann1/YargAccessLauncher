# YargAccess licences

YargAccess is free software: you can redistribute it and/or modify it under the terms of the GNU
General Public License as published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version. See `LICENSE` for the full text.

YargAccess is distributed in the hope that it will be useful, but WITHOUT ANY WARRANTY; without even
the implied warranty of MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the GNU General
Public License for more details.

Copyright (C) 2026 Michael Johann

## Additional permission under GNU GPL version 3 section 7

YargAccess runs inside YARG and talks to libraries that are not under the GPL: the Unity engine,
the BASS audio library, MelonLoader and its Harmony, and the speech libraries listed below. If you
modify this program, or any covered work, by linking or combining it with any of those libraries
(or a modified version of them), the licensors of this program grant you additional permission to
convey the resulting work. Corresponding Source for a non-source form of such a combination shall
not include the source code for the parts of those libraries used as well as that of the covered
work.

## What the mod works with

The mod is a plug-in. It is built against these, but does not include them, except the speech
libraries, which the release zip carries.

| Component | Licence | Where |
|---|---|---|
| YARG (Yet Another Rhythm Game), by YARC | GNU LGPL v3.0 or later | https://github.com/YARC-Official/YARG |
| YARG.Core | GNU LGPL v3.0 or later | https://github.com/YARC-Official/YARG.Core |
| MelonLoader, by LavaGang | Apache License 2.0 | https://github.com/LavaGang/MelonLoader |
| HarmonyX (0Harmony), shipped with MelonLoader | MIT | https://github.com/BepInEx/HarmonyX |
| Unity engine | Unity's proprietary terms, part of YARG | https://unity.com/legal |
| BASS and ManagedBass, shipped with YARG | BASS: proprietary, free for non-commercial use; ManagedBass: MIT | https://www.un4seen.com |

## Shipped in the release zip

| File | What it is | Licence |
|---|---|---|
| `UniversalSpeech.dll` | UniversalSpeech, by Quentin Cosendey: speaks through the running screen reader | See the project's own licence: https://github.com/qtnc/UniversalSpeech |
| `nvdaControllerClient.dll` | NVDA Controller Client, by NV Access | GNU LGPL v2.1, https://github.com/nvaccess/nvda |
| `nvdaControllerClient64.dll` | NVDA Controller Client, by NV Access (the newer client; YargAccess speaks through it, so its lines appear in NVDA's speech history) | GNU LGPL v2.1, https://github.com/nvaccess/nvda |
| `ZDSRAPI.dll` | Zhengdu (ZDSR) screen reader API | The vendor's terms, https://www.zdsr.com |

YARG and its name and logos belong to their owners. YargAccess is an independent accessibility mod
for YARG and is not made or endorsed by YARC.
