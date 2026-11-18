# YargAccess

YargAccess is a screen reader mod for [YARG](https://yarg.in), the free rhythm game. Your screen
reader reads every menu, and you can play songs by ear: each note, pad or key makes its own sound
just before you need to hit it. Five-fret guitar and bass, drums, pro keys and vocals all work.

You don't have to set any of this up by hand. The YARG Access launcher installs YARG, MelonLoader,
the mod and its speech files, keeps them up to date, and downloads songs for you. This read me
covers both. The mod's code is in `MichaelJohann1/YargAccess` and the launcher's in
`MichaelJohann1/YargAccessLauncher`.

## Using the launcher

1. Download `YargAccessLauncher-<version>.zip` from the launcher's latest release and unzip it
   wherever you like.
2. Run `YargAccessLauncher.exe`. It talks through your screen reader.
3. On the Game tab, choose Install or update YARG and the accessibility mod, then Play YARG.

The launcher has six tabs. Control+Tab and Control+Shift+Tab move between them, and the launcher
says the name of each tab as you get there.

- Game: choose the Stable or Nightly build of YARG, then install, update or play it. Use Stable.
  The mod is built and tested against Stable, and Nightly changes often, so the mod will most
  likely not work with it. You can also repair the mod (this puts MelonLoader, the mod and the
  speech files back), reinstall YARG, open the game folder or the logs folder, or uninstall.
- News: YARG's news articles.
- DLC: YARG's official setlists and venues. Install, update or delete them here.
- Download songs through Chorus: search Chorus Encore for charts, hear a preview played with the
  mod's note sounds, and download a chart into your songs folder.
- Documents: this read me, the changelogs, the contributing guide and the licences.
- Settings: your folders, which YARG install to manage, the language YARG starts in, where the mod
  is downloaded from, and whether download progress is spoken.

Anywhere in the launcher, F1 lists the keys and F2 reads how far a download has got. F3 turns
spoken progress on or off. Escape cancels a download or stops a preview.

The launcher keeps its settings and log in `%APPDATA%\YARG Access` (`config.json` and
`launcher.log`).

YARG has its own updater, and when it replaces the game folder the mod goes with it. Choose Repair
the accessibility mod on the Game tab to get it back.

## Languages

YARG comes in nine languages: English, Spanish (Spain), French, Italian, Japanese, Korean, Polish,
and Portuguese (Brazil and Portugal). It has no language setting of its own; it starts in the
language it is given when it launches. Choose one with YARG language on the launcher's Settings
tab, choose Save settings, then Play YARG.

The mod speaks the same language as YARG: its menus, help, tutorials, practice and messages are
translated into each of the nine. Your screen reader speaks the text, so set its voice to one that
speaks that language.

Some things stay in English or as they are:

- Parts of YARG itself. YARG's own translations leave some screens in English, such as the pause
  menu (Resume, Restart, Save Replay, Back To Library), the profile settings (Lefty Flip, Note
  Speed, Instrument Select and the rest) and the preset names. The mod says those names in English
  too, so what you hear matches the screen. YARG's Korean translation is mostly unfinished, so in
  Korean most menu and setting names are in English.
- A few YARG messages that YARG only writes in English, such as "Device added". The mod speaks
  them in your language, but the screen still shows them in English.
- Songs, artists, anything a chart's author wrote, and the names of your devices, microphones and
  controls.
- Controller buttons (A, B, X, Y, LB, RB, LT, RT, View, Start), letter keys and F keys.
- Numbers with a decimal point are said with a dot, for example 0.15 seconds.

Grammar has limits too: a name or number dropped into a sentence can't change its ending to fit,
so a few lines are worded a little stiffly, and in Polish some counts are read as "label: number".

YARG also starts in a language from the command line, for example `YARG.exe -lang fr-FR`, and the
mod follows that too.

These translations were made and checked with AI.

## Get some songs first

YARG doesn't come with any songs. Until you download at least one setlist there is nothing to
play, and the menus are silent too, because the menu music comes from your own songs.

The launcher can get them for you. The DLC tab has YARG's official setlists, and the Download
songs through Chorus tab finds single charts made by the community. If YARG was open while you
downloaded, choose Scan Songs on the Songs tab of YARG's Settings, or restart the game.

Songs you already have, from Clone Hero or anywhere else, go in a song folder you add in YARG's
Settings. The Songs and Folders tutorial in the game explains adding, scanning and removing
folders, and how that differs from what the launcher does.

## The tutorials in the game

Once you're in YARG, the mod can teach you the rest. The first time the main menu comes up, it
asks whether you want the tutorials. You can open them later from Tutorials on the main menu,
just below Practice.

They cover how the mod works, how these games work, each instrument, the play styles, scoring,
star power, practice mode and replays. Each instrument tutorial ends with a practice: you hear
every kind of note, then play it back on your own controller. The same list has a Sound Glossary,
where you can hear every sound the mod makes, and Practice The Buttons. In any menu, F1 tells you
the keys.

## Getting help

If something doesn't work, or you're stuck, join [this Discord server](https://discord.gg/V8tvAN84Mr)
and ask in the yarg-support channel.

You must attach the mod's log to your message. Without it, it is very difficult to help you.

### Where the logs are

Choose Open the logs folder on the launcher's Game tab. It opens the `MelonLoader` folder inside
the game folder, which holds:

- `Latest.log`: the log of the last time you played, or of the game that is running now. It is
  replaced every time YARG starts.
- A `Logs` folder with a copy of every earlier log. Each file is named after the date and time
  the game started, year first: `26-11-16_15-57-52.log` is from 16 November 2026 at 15:57.

### Sending the log

1. Close YARG, so the log is complete.
2. Choose Open the logs folder on the launcher's Game tab.
3. If you haven't started YARG since the problem, send `Latest.log`. If you have, open the `Logs`
   folder and send the file whose date and time match the game where it happened.
4. In the yarg-support channel, attach the file to your message (the plus button next to the
   message box, or drag the file in), and say what you were doing and what went wrong.

## Contributing

To build the mod or the launcher yourself, or to send a change, read `CONTRIBUTING.md` (on the
launcher's Documents tab, Contributing).

## Licence

The mod and the launcher are free software under the GNU General Public License version 3 or later.
An extra permission lets them run alongside YARG, Unity, BASS, MelonLoader and the speech
libraries. The licence text is in `LICENSE` in each repository. The extra permission and the
licences of the parts they use are in `THIRD-PARTY-LICENSES.md`. YARG itself is a separate project
by YARC, with its own licence.

Copyright (C) 2026 Michael Johann

## Made with AI

Claude, Anthropic's AI model, wrote these with Claude Code:

- All the source code of the mod and the launcher, and their build scripts.
- Everything the mod says and shows: the tutorials, the help, the descriptions and the messages.
- `README.md`, `CONTRIBUTING.md` and `CHANGELOG.md` in both repositories.
- `THIRD-PARTY-LICENSES.md` in both repositories. The extra permission in it follows the Free
  Software Foundation's standard wording for a GPL section 7 exception.

`LICENSE` is the GNU General Public License as published by the Free Software Foundation, unchanged;
it was not written by AI.

Michael Johann directs the work, plays and tests every change by ear with a screen reader, and
decides what ships. There will be the odd mistake, so please report any you find on Discord.
