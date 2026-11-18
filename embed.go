// Package launcher holds the files built into the YARG Access launcher: MelonLoader's Loader.cfg,
// the mod's changelog and licences (copied from the mod repository by tools/sync-mod-docs.ps1) and
// the launcher's read me (the same as the mod's, copied by the same script), changelog and licences,
// which the Documents tab shows.
package launcher

import "embed"

//go:embed data/Loader.cfg data/mod/CHANGELOG.md data/mod/CONTRIBUTING.md data/mod/LICENSE data/mod/THIRD-PARTY-LICENSES.md
//go:embed README.md CHANGELOG.md LICENSE THIRD-PARTY-LICENSES.md
var Files embed.FS

// LoaderCfg is MelonLoader's settings file with the console hidden.
func LoaderCfg() []byte {
	b, _ := Files.ReadFile("data/Loader.cfg")
	return b
}
