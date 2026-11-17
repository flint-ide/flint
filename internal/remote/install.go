package remote

import (
	"fmt"

	"flint/internal/agenthost"
	"flint/internal/settings"
)

// The install instructions for the headless host are generated from these
// constants and nothing else. Flint installs nothing by itself: it shows the
// steps and the user runs them.
//
// No release pipeline publishes the archives yet. ArtifactFormat is the name
// they WILL have on the releases page; ReleasesURL is the page, not a link to
// a file, so no URL that does not exist is invented. When releases publish the
// archives, nothing here changes but the Notice.
const (
	// ReleasesURL is the repository's releases page.
	ReleasesURL = "https://github.com/flint-ide/flint/releases"
	// ArtifactFormat is the archive name for an OS and an architecture
	// ("flint-host-linux-amd64.tar.gz"); it holds one file, "flint".
	ArtifactFormat = "flint-host-%s-%s.tar.gz"
	// InstallDir is where the steps put the binary on the machine.
	InstallDir = "~/.local/bin"
	// InstalledCommand is the "command" to set for a machine after the steps.
	InstalledCommand = InstallDir + "/flint"
	// Sha256Placeholder stands for the SHA-256 shown next to the archive on
	// the releases page.
	Sha256Placeholder = "<sha256 shown for this file on the releases page>"
	// Notice is shown above the steps until releases publish the archives.
	Notice = "No release publishes these archives yet. The file names below are where releases will publish them; " +
		"until then build one yourself with `wails3 task host:package` (see docs/DEVELOPMENT.md) and copy it to the machine."
)

// Platforms are the OS and architecture pairs the headless host is built for.
var Platforms = [][2]string{{"linux", "amd64"}, {"linux", "arm64"}, {"darwin", "amd64"}, {"darwin", "arm64"}}

// Artifact is the archive name for a platform.
func Artifact(goos, goarch string) string { return fmt.Sprintf(ArtifactFormat, goos, goarch) }

// InstallStep is one step: what it is for, and the command to run on the
// machine ("" for a step done in a browser).
type InstallStep struct {
	Title   string `json:"title"`
	Command string `json:"command"`
	Note    string `json:"note"`
}

// InstallInstructions is what the Settings page shows when a machine has no
// usable Flint.
type InstallInstructions struct {
	Notice      string        `json:"notice"`
	ReleasesURL string        `json:"releasesUrl"`
	Artifacts   []string      `json:"artifacts"`
	Steps       []InstallStep `json:"steps"`
	// Command is the value to put in the machine's "command" setting.
	Command string `json:"command"`
}

// Install returns the steps that install the headless host on machine m. They
// are text only: nothing runs.
func Install(settings.Machine) InstallInstructions {
	names := make([]string, 0, len(Platforms))
	for _, p := range Platforms {
		names = append(names, Artifact(p[0], p[1]))
	}
	const example = "flint-host-<os>-<arch>.tar.gz"
	return InstallInstructions{
		Notice: Notice, ReleasesURL: ReleasesURL, Artifacts: names, Command: InstalledCommand,
		Steps: []InstallStep{
			{
				Title: "Find the machine's system and architecture", Command: "uname -sm",
				Note: "Linux x86_64 is linux-amd64, Linux aarch64 is linux-arm64, Darwin x86_64 is darwin-amd64, Darwin arm64 is darwin-arm64.",
			},
			{
				Title: "Download the archive for it from the releases page, onto the machine",
				Note:  ReleasesURL + " (" + example + "). Copy it over with scp if the machine cannot reach the internet.",
			},
			{
				Title:   "Check the SHA-256 of the archive against the one on the releases page",
				Command: fmt.Sprintf("echo \"%s  %s\" | sha256sum -c -", Sha256Placeholder, example),
				Note:    "On macOS use `shasum -a 256 -c -` instead of `sha256sum -c -`. Do not go on if it does not say OK.",
			},
			{
				Title:   "Unpack it into " + InstallDir,
				Command: fmt.Sprintf("mkdir -p %s && tar -xzf %s -C %s", InstallDir, example, InstallDir),
				Note:    "The archive holds one file, flint.",
			},
			{
				Title:   "Run it once",
				Command: InstalledCommand + " --version",
				Note:    fmt.Sprintf("It prints: flint <version> host-protocol %d. This Flint needs host protocol %d.", agenthost.Protocol, agenthost.Protocol),
			},
			{
				Title: "Set this machine's command and Check again",
				Note:  fmt.Sprintf("Set Command to %s (a non-interactive ssh shell often does not have %s on its PATH), then press Check.", InstalledCommand, InstallDir),
			},
		},
	}
}
