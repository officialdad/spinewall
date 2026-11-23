// Package runtime fetches, verifies and caches the Spine players the page loads.
//
// The runtime license forbids redistribution, so the players are never embedded
// or committed. This package downloads them on the user's machine, checks each
// download against a checked-in sha256 pin, transforms the global name, and
// records the transformed bytes in a cache manifest. The pins mirror
// scripts/install.sh, which stays as a shim until the installer phase removes it.
package runtime

// Source is where a pinned player comes from. The two sources need different
// transforms: an npm tarball is un-tarred and renamed, a raw 3.x file is wrapped.
type Source int

const (
	// Tarball is an npm package tarball holding package/dist/iife/spine-player.js.
	Tarball Source = iota
	// RawFile is one spine-player.js file from a pinned spine-runtimes commit.
	RawFile
)

const (
	npmBase = "https://registry.npmjs.org"
	rawBase = "https://raw.githubusercontent.com"
)

// Pin is one checked-in runtime download. SHA256 is the hash of the download
// bytes before any transform, exactly as install.sh checks it.
type Pin struct {
	// Global is the JavaScript global the page reads, such as spine41.
	Global string
	// Version labels the player for logs and the manifest.
	Version string
	// Source selects the download shape and the transform.
	Source Source
	// Path is appended to the source host. A Tarball path is an npm registry
	// path; a RawFile path is a raw.githubusercontent.com path.
	Path string
	// SHA256 is the lowercase hex digest of the download bytes.
	SHA256 string
}

// Pins are the runtimes this build knows how to fetch. The order matches
// scripts/install.sh so a shim install and a binary install produce the same set.
var Pins = []Pin{
	{
		Global:  "spine40",
		Version: "4.0.31",
		Source:  Tarball,
		Path:    "@esotericsoftware/spine-player/-/spine-player-4.0.31.tgz",
		SHA256:  "db13daf8219b1de211785ed6bf70588b66879168cd9b7406aa62b30cef56d8b1",
	},
	{
		Global:  "spine41",
		Version: "4.1.56",
		Source:  Tarball,
		Path:    "@esotericsoftware/spine-player/-/spine-player-4.1.56.tgz",
		SHA256:  "5c6d530857d6eb18d8bb5f8b10e4ba62fed42e887bf874088e91fe57f2307a80",
	},
	{
		Global:  "spine42",
		Version: "4.2.120",
		Source:  Tarball,
		Path:    "@esotericsoftware/spine-player/-/spine-player-4.2.120.tgz",
		SHA256:  "f553d60910e6069b121555d4575c60f31c65d6fd2a3e349328ba9d2882dd79f1",
	},
	{
		Global:  "spine38",
		Version: "3.8",
		Source:  RawFile,
		Path:    "EsotericSoftware/spine-runtimes/8b4844bd4b193ba9e54487ed397a777993cbad56/spine-ts/build/spine-player.js",
		SHA256:  "3f335337a8fa9c51c6502a7557ea44cd2bcb4f22483adae275e4ecf5ec69faec",
	},
	{
		Global:  "spine37",
		Version: "3.7",
		Source:  RawFile,
		Path:    "EsotericSoftware/spine-runtimes/9639bcc81722d7178fd9d1cdc1a3d55a4c91f989/spine-ts/build/spine-player.js",
		SHA256:  "7e94ec3760c98013b2eb4449fc9ad0fe84b7a3cbe9c4368e326c7339628a3feb",
	},
}

// pinFor returns the pin for a global, or false when the name is unknown.
func pinFor(global string) (Pin, bool) {
	for _, pin := range Pins {
		if pin.Global == global {
			return pin, true
		}
	}
	return Pin{}, false
}

// cssPin is the runtime whose tarball supplies spine-player.css when no other
// 4.x player is being fetched. The 4.x packages ship the same stylesheet.
const cssPin = "spine42"
