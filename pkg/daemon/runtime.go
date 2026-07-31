package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Runtime detection: which container image should run this repo's test command.
//
// Kiwi's promise is that a user submits a prompt and nothing else, so this can
// never fall back to asking them. It reads what the repository already declares
// — the same files its own developers and CI rely on — and, when that is not
// enough, corrects itself from what the sandbox reports (see envFault below).
//
// Nothing here calls a model. Marker files are labels, not judgements, and the
// planner is deliberately not involved: it is handed the repo URL, never its
// contents, so an image chosen there would be the same guess that already sent
// three jobs to the wrong file, the wrong model and the wrong test command.

// Default image versions, used when the repository pins nothing. Deliberately
// conservative: a current, widely-deployed release rather than the newest.
const (
	defaultGoVersion     = "1.25"
	defaultNodeVersion   = "20"
	defaultPythonVersion = "3.12"
)

// Fallback when a repository declares no recognisable ecosystem at all. Go is
// the historical default and stays, but at a version that can actually build a
// modern module — the previous hardcoded golang:1.21-alpine could not compile
// Kiwi itself ("go.mod requires go >= 1.25.0").
func defaultImage() string { return goImage(defaultGoVersion) }

func goImage(v string) string     { return "golang:" + v + "-alpine" }
func nodeImage(v string) string   { return "node:" + v + "-alpine" }
func pythonImage(v string) string { return "python:" + v + "-slim" }

// Ecosystems whose images carry no version we can usefully derive from the repo.
const (
	rustImage   = "rust:1-alpine"
	mavenImage  = "maven:3-eclipse-temurin-21"
	gradleImage = "gradle:8-jdk21"
	rubyImage   = "ruby:3-alpine"
	phpImage    = "php:8-cli-alpine"
)

// ecosystem is an internal label for a language toolchain, resolved to a
// concrete image (with a version read from the repo) by imageFor.
type ecosystem string

const (
	ecoGo     ecosystem = "go"
	ecoNode   ecosystem = "node"
	ecoPython ecosystem = "python"
	ecoRust   ecosystem = "rust"
	ecoMaven  ecosystem = "maven"
	ecoGradle ecosystem = "gradle"
	ecoRuby   ecosystem = "ruby"
	ecoPHP    ecosystem = "php"
)

// commandEcosystem maps the leading executable of a test command to the
// toolchain that must be present to run it. This is the strongest signal
// available: the command names the binary it needs, so an image without that
// binary is wrong no matter what the repository's marker files suggest. It
// settles polyglot repos — a Go service with a frontend has both go.mod and
// package.json, and only the command says which one is being tested.
var commandEcosystem = map[string]ecosystem{
	"go":  ecoGo,
	"npm": ecoNode, "npx": ecoNode, "node": ecoNode, "yarn": ecoNode, "pnpm": ecoNode,
	"python": ecoPython, "python3": ecoPython, "pip": ecoPython, "pip3": ecoPython,
	"pytest": ecoPython, "tox": ecoPython, "poetry": ecoPython, "uv": ecoPython,
	"cargo": ecoRust, "rustc": ecoRust,
	"mvn": ecoMaven, "mvnw": ecoMaven,
	"gradle": ecoGradle, "gradlew": ecoGradle,
	"bundle": ecoRuby, "ruby": ecoRuby, "rake": ecoRuby, "rspec": ecoRuby,
	"php": ecoPHP, "composer": ecoPHP, "phpunit": ecoPHP,
}

// inferSandboxImage picks the image that runs a repository's test command.
//
// Order, most authoritative first:
//
//  1. devcontainer.json — an explicit image, written by the repo's own authors
//     for exactly this purpose.
//  2. The test command's executable — it names the binary that must exist.
//  3. Marker files — go.mod, package.json, and friends.
//  4. The default, so this never returns nothing and never has to ask.
func inferSandboxImage(dir, testCmd string) string {
	if img := devcontainerImage(dir); img != "" {
		return img
	}
	return imageFor(inferEcosystem(dir, testCmd), dir)
}

// inferEcosystem resolves the toolchain a repository's tests need. Separate
// from image selection because the package cache is wired per ecosystem too,
// and both must agree about which one this is.
func inferEcosystem(dir, testCmd string) ecosystem {
	if eco, ok := commandEcosystem[leadingCommand(testCmd)]; ok {
		return eco
	}
	if eco, ok := markerEcosystem(dir); ok {
		return eco
	}
	return ecoGo
}

// leadingCommand extracts the executable a shell command invokes, seeing past
// leading environment assignments ("CI=true npm test") and a ./ prefix.
func leadingCommand(cmd string) string {
	for _, field := range strings.Fields(cmd) {
		if strings.Contains(field, "=") && !strings.HasPrefix(field, "-") {
			continue // FOO=bar prefix
		}
		return strings.TrimPrefix(filepath.Base(field), "./")
	}
	return ""
}

// markerEcosystem identifies a toolchain from the files a project keeps at its
// root. Order matters only for repositories carrying several ecosystems'
// markers, and there the test command (checked first by the caller) is the
// signal that actually decides.
func markerEcosystem(dir string) (ecosystem, bool) {
	exists := func(name string) bool {
		_, err := os.Stat(filepath.Join(dir, name))
		return err == nil
	}
	switch {
	case exists("go.mod"):
		return ecoGo, true
	case exists("package.json"):
		return ecoNode, true
	case exists("pyproject.toml"), exists("setup.py"), exists("requirements.txt"),
		exists("pytest.ini"), exists("tox.ini"), exists("Pipfile"):
		return ecoPython, true
	case exists("Cargo.toml"):
		return ecoRust, true
	case exists("pom.xml"):
		return ecoMaven, true
	case exists("build.gradle"), exists("build.gradle.kts"):
		return ecoGradle, true
	case exists("Gemfile"):
		return ecoRuby, true
	case exists("composer.json"):
		return ecoPHP, true
	}
	return "", false
}

func imageFor(eco ecosystem, dir string) string {
	switch eco {
	case ecoGo:
		return goImage(firstNonEmpty(goModVersion(dir), defaultGoVersion))
	case ecoNode:
		return nodeImage(firstNonEmpty(nodeVersion(dir), defaultNodeVersion))
	case ecoPython:
		return pythonImage(firstNonEmpty(pythonVersion(dir), defaultPythonVersion))
	case ecoRust:
		return rustImage
	case ecoMaven:
		return mavenImage
	case ecoGradle:
		return gradleImage
	case ecoRuby:
		return rubyImage
	case ecoPHP:
		return phpImage
	}
	return defaultImage()
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// devcontainerImage reads an explicit image from a devcontainer definition.
// Only the plain "image" form is honoured: a devcontainer that builds from a
// Dockerfile describes a build we are not performing, and guessing at its
// result would be worse than falling through to the rest of the ladder.
func devcontainerImage(dir string) string {
	for _, p := range []string{
		filepath.Join(dir, ".devcontainer", "devcontainer.json"),
		filepath.Join(dir, ".devcontainer.json"),
	} {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		var dc struct {
			Image string `json:"image"`
		}
		// devcontainer.json permits comments and trailing commas, which
		// encoding/json rejects. A file we cannot parse is simply skipped —
		// the ladder below still produces a usable image.
		if err := json.Unmarshal(stripJSONComments(data), &dc); err != nil {
			continue
		}
		if img := strings.TrimSpace(dc.Image); img != "" {
			return img
		}
	}
	return ""
}

var lineCommentRe = regexp.MustCompile(`(?m)^\s*//.*$`)

func stripJSONComments(b []byte) []byte {
	return lineCommentRe.ReplaceAll(b, nil)
}

var goDirectiveRe = regexp.MustCompile(`(?m)^\s*go\s+(\d+)\.(\d+)`)

// goModVersion reads the language version from a go.mod `go` directive, as
// major.minor — the granularity Docker tags use.
func goModVersion(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
	if err != nil {
		return ""
	}
	if m := goDirectiveRe.FindSubmatch(data); m != nil {
		return string(m[1]) + "." + string(m[2])
	}
	return ""
}

var majorRe = regexp.MustCompile(`(\d+)`)

// nodeVersion reads a Node major version from .nvmrc, then package.json's
// engines.node. Symbolic .nvmrc values ("lts/iron") name a release line we
// cannot map to a tag without a lookup table that would go stale, so they fall
// through to the default.
func nodeVersion(dir string) string {
	if data, err := os.ReadFile(filepath.Join(dir, ".nvmrc")); err == nil {
		v := strings.TrimSpace(string(data))
		if !strings.HasPrefix(strings.ToLower(v), "lts") {
			if m := majorRe.FindString(v); m != "" {
				return m
			}
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return ""
	}
	var pkg struct {
		Engines struct {
			Node string `json:"node"`
		} `json:"engines"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return ""
	}
	// Ranges like ">=18", "^20.0.0" and "20.x" all yield their first number,
	// which is the floor the project supports and a tag that exists.
	return majorRe.FindString(pkg.Engines.Node)
}

var pyVersionRe = regexp.MustCompile(`(\d+)\.(\d+)`)

func pythonVersion(dir string) string {
	data, err := os.ReadFile(filepath.Join(dir, ".python-version"))
	if err != nil {
		return ""
	}
	if m := pyVersionRe.FindSubmatch(data); m != nil {
		return string(m[1]) + "." + string(m[2])
	}
	return ""
}

// extensionEcosystem maps a source-file extension to the toolchain it belongs
// to. Only unambiguous, language-defining extensions appear here: a Go project
// legitimately contains .md, .yaml and .json files, and "correcting" those
// would be worse than the bug this exists to fix.
var extensionEcosystem = map[string]ecosystem{
	".go":   ecoGo,
	".rs":   ecoRust,
	".py":   ecoPython,
	".rb":   ecoRuby,
	".php":  ecoPHP,
	".java": ecoMaven,
	".js":   ecoNode, ".jsx": ecoNode, ".ts": ecoNode, ".tsx": ecoNode, ".mjs": ecoNode,
}

// primaryExtension is the extension a new source file should carry for an
// ecosystem. Node is resolved against the repository because .ts and .js are
// both correct and only the project can say which.
func primaryExtension(eco ecosystem, dir string) string {
	switch eco {
	case ecoGo:
		return ".go"
	case ecoRust:
		return ".rs"
	case ecoPython:
		return ".py"
	case ecoRuby:
		return ".rb"
	case ecoPHP:
		return ".php"
	case ecoMaven, ecoGradle:
		return ".java"
	case ecoNode:
		if _, err := os.Stat(filepath.Join(dir, "tsconfig.json")); err == nil {
			return ".ts"
		}
		return ".js"
	}
	return ""
}

// correctNewFileExtension repairs a path for a file that does not exist yet when
// its extension belongs to a different language than the repository's.
//
// The planner names files without seeing the repo, so it guesses — and a guess
// can be wrong in a way nothing downstream can recover from. It planned
// examples/advanced.rs for a Go project; the Actor may only change a file's
// CONTENTS, never its name, so the Critic correctly rejected the result three
// times ("contains Go code, but has a .rs file extension") and the task burned
// its entire ten-minute budget on a position it could not win.
//
// Deliberately narrow: it fires only for a file being created, only when the
// extension unambiguously names another language, and only when we have a
// confident replacement. Anything else — .md, .yaml, no extension at all — is
// left exactly as it is.
func correctNewFileExtension(rel string, eco ecosystem, dir string) string {
	ext := strings.ToLower(filepath.Ext(rel))
	if ext == "" {
		return rel
	}
	fileEco, known := extensionEcosystem[ext]
	if !known || fileEco == eco {
		return rel
	}
	want := primaryExtension(eco, dir)
	if want == "" || want == ext {
		return rel
	}
	return strings.TrimSuffix(rel, filepath.Ext(rel)) + want
}
