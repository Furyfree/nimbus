package selector

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestNormalizeOrigin(t *testing.T) {
	want := "github.com/furyfree-org/nimbus"
	inputs := []string{
		"git@github.com:furyfree-org/nimbus.git",
		"git@GitHub.com:furyfree-org/nimbus",
		"https://github.com/furyfree-org/nimbus",
		"https://github.com/furyfree-org/nimbus.git",
		"https://github.com/furyfree-org/nimbus/",
		"https://user@github.com/furyfree-org/nimbus.git",
		"ssh://git@github.com/furyfree-org/nimbus.git",
		"github.com/furyfree-org/nimbus",
	}
	for _, in := range inputs {
		got, err := NormalizeOrigin(in)
		if err != nil {
			t.Errorf("%q: %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("%q -> %q, want %q", in, got, want)
		}
	}
	for _, in := range []string{"", "ftp://x/y", "nimbus", "file:///tmp/x"} {
		if _, err := NormalizeOrigin(in); err == nil {
			t.Errorf("%q: expected an error", in)
		}
	}
}

func writeSelector(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "config.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadSelector(t *testing.T) {
	good := "schema = 1\ncheckout = \"/tmp/x\"\nmachine = \"desktop\"\norigin = \"github.com/furyfree-org/nimbus\"\n"
	if _, err := Load(writeSelector(t, good)); err != nil {
		t.Fatal(err)
	}
	bad := map[string]string{
		"missing origin":     "schema = 1\ncheckout = \"/tmp/x\"\nmachine = \"desktop\"\n",
		"unnormalized":       "schema = 1\ncheckout = \"/tmp/x\"\nmachine = \"desktop\"\norigin = \"https://github.com/a/b.git\"\n",
		"unknown field":      good + "profiles = [\"common\"]\n",
		"unsupported schema": strings.Replace(good, "schema = 1", "schema = 2", 1),
	}
	for name, body := range bad {
		if _, err := Load(writeSelector(t, body)); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestWriteSelectorRoundTripsCheckoutPaths(t *testing.T) {
	for _, tc := range []struct{ name, directory string }{
		{"ordinary", "checkout"},
		{"unicode", "checkout-\u00e6\u65e5"},
		{"quotes", "checkout-\"quoted'"},
		{"backslash", `checkout\path`},
		{"whitespace", "checkout\t\n\r"},
		{"bell", "checkout\a"},
		{"vertical tab", "checkout\v"},
		{"delete", "checkout\x7f"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkout := filepath.Join(t.TempDir(), tc.directory)
			if err := os.Mkdir(checkout, 0o700); err != nil {
				t.Fatal(err)
			}
			want := &Selector{Schema: CurrentSchema, Checkout: checkout, Machine: "desktop", Origin: "github.com/Furyfree/nimbus"}
			path := filepath.Join(t.TempDir(), "nimbus", "config.toml")
			if err := Write(path, want); err != nil {
				t.Fatal(err)
			}
			got, err := Load(path)
			if err != nil {
				t.Fatalf("load written selector: %v", err)
			}
			if *got != *want {
				t.Fatalf("selector = %+v, want %+v", got, want)
			}
		})
	}
}

func TestWriteRejectsInvalidUTF8BeforeMutation(t *testing.T) {
	for _, field := range []string{"checkout", "machine", "origin"} {
		t.Run(field, func(t *testing.T) {
			valid := Selector{Schema: CurrentSchema, Checkout: t.TempDir(), Machine: "desktop", Origin: "github.com/Furyfree/nimbus"}
			invalid := valid
			switch field {
			case "checkout":
				invalid.Checkout += "\xff"
			case "machine":
				invalid.Machine += "\xff"
			case "origin":
				invalid.Origin += "\xff"
			}
			path := filepath.Join(t.TempDir(), "nimbus", "config.toml")
			if err := Write(path, &invalid); err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
				t.Fatalf("invalid %s accepted: %v", field, err)
			}
			if _, err := os.Stat(filepath.Dir(path)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid selector created its directory: %v", err)
			}
			if err := Write(path, &valid); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := Write(path, &invalid); err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
				t.Fatalf("invalid %s accepted for replacement: %v", field, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || string(after) != string(before) {
				t.Fatalf("existing selector changed: %q, %v", after, err)
			}
		})
	}
}

func TestWriteRemovesTemporaryFileOnRenameFailure(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	preserved := filepath.Join(path, "preserved")
	if err := os.WriteFile(preserved, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := Write(path, &Selector{Schema: CurrentSchema, Checkout: "checkout", Machine: "desktop", Origin: "github.com/Furyfree/nimbus"})
	if rename, ok := errors.AsType[*os.LinkError](err); !ok || rename.Op != "rename" {
		t.Fatalf("expected rename failure, got %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "config.toml" {
		t.Fatalf("temporary file remains after failed rename: %v", entries)
	}
	content, err := os.ReadFile(preserved)
	if err != nil || string(content) != "keep" {
		t.Fatalf("destination changed: %q, %v", content, err)
	}
}

func gitCheckout(t *testing.T, config string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".git", "config"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	return root
}

const originConfig = "[core]\n\trepositoryformatversion = 0\n[remote \"origin\"]\n\turl = git@github.com:furyfree-org/nimbus.git\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n"

func TestVerify(t *testing.T) {
	sel := &Selector{Schema: 1, Checkout: "x", Machine: "m", Origin: "github.com/furyfree-org/nimbus"}
	if err := Verify(sel, gitCheckout(t, originConfig)); err != nil {
		t.Fatal(err)
	}
	other := strings.Replace(originConfig, "furyfree-org/nimbus", "someone/else", 1)
	if err := Verify(sel, gitCheckout(t, other)); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("mismatch not reported: %v", err)
	}
	if err := Verify(sel, gitCheckout(t, "[core]\n")); err == nil {
		t.Fatal("missing origin not reported")
	}
	if err := Verify(sel, gitCheckout(t, "[include]\n\tpath = other\n"+originConfig)); err == nil || !strings.Contains(err.Error(), "include") {
		t.Fatalf("include not rejected: %v", err)
	}
	if err := Verify(sel, t.TempDir()); err == nil {
		t.Fatal("non-repository accepted")
	}
}

func TestVerifyWorktreePointer(t *testing.T) {
	main := gitCheckout(t, originConfig)
	wtDir := filepath.Join(main, ".git", "worktrees", "wt")
	if err := os.MkdirAll(wtDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "commondir"), []byte("../..\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+wtDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sel := &Selector{Schema: 1, Checkout: wt, Machine: "m", Origin: "github.com/furyfree-org/nimbus"}
	if err := Verify(sel, wt); err != nil {
		t.Fatal(err)
	}
}

func TestSelectorMustBeARegularFile(t *testing.T) {
	good := "schema = 1\ncheckout = \"/tmp/x\"\nmachine = \"desktop\"\norigin = \"github.com/furyfree-org/nimbus\"\n"
	real := writeSelector(t, good)
	link := filepath.Join(t.TempDir(), "config.toml")
	if err := os.Symlink(real, link); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if _, err := Load(link); err == nil || !strings.Contains(err.Error(), "symlink") {
		t.Fatalf("symlinked selector accepted: %v", err)
	}
	if _, err := Load(t.TempDir()); err == nil {
		t.Fatal("directory accepted as selector")
	}
}

func TestOriginKeyIsCaseInsensitive(t *testing.T) {
	sel := &Selector{Schema: 1, Checkout: "x", Machine: "m", Origin: "github.com/furyfree-org/nimbus"}
	upper := strings.Replace(originConfig, "\turl =", "\tURL =", 1)
	if err := Verify(sel, gitCheckout(t, upper)); err != nil {
		t.Fatal(err)
	}
}

func TestRemoteSubsectionIsCaseSensitive(t *testing.T) {
	sel := &Selector{Schema: 1, Checkout: "x", Machine: "m", Origin: "github.com/furyfree-org/nimbus"}
	cased := strings.Replace(originConfig, `[remote "origin"]`, `[remote "Origin"]`, 1)
	if err := Verify(sel, gitCheckout(t, cased)); err == nil || !strings.Contains(err.Error(), "not set") {
		t.Fatalf("differently cased remote accepted: %v", err)
	}
	upperSection := strings.Replace(originConfig, `[remote "origin"]`, `[Remote "origin"]`, 1)
	if err := Verify(sel, gitCheckout(t, upperSection)); err != nil {
		t.Fatalf("section name must be case-insensitive: %v", err)
	}
}

func TestUnreadableCommondirFails(t *testing.T) {
	main := gitCheckout(t, originConfig)
	wtDir := filepath.Join(main, ".git", "worktrees", "wt")
	if err := os.MkdirAll(filepath.Join(wtDir, "commondir"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wtDir, "config"), []byte(originConfig), 0o644); err != nil {
		t.Fatal(err)
	}
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+wtDir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	sel := &Selector{Schema: 1, Checkout: wt, Machine: "m", Origin: "github.com/furyfree-org/nimbus"}
	if err := Verify(sel, wt); err == nil || !strings.Contains(err.Error(), "commondir") {
		t.Fatalf("directory commondir fell back to the worktree config: %v", err)
	}
}

func TestOriginSectionSyntax(t *testing.T) {
	for _, header := range []string{
		`[remote "origin"] # origin comment`,
		"[Remote\t\"origin\"] ; origin comment",
		`[remote  "origin"]`,
		`[remote "or\igin"]`,
	} {
		t.Run(header, func(t *testing.T) {
			config := header + "\nurl=https://github.com/Furyfree/nimbus.git\n" +
				`[remote "other] # ; \" \\"] # outside comment` + "\nurl=https://github.com/other/repo.git\n"
			got, err := ParseOriginURL([]byte(config))
			if err != nil || got != "https://github.com/Furyfree/nimbus.git" {
				t.Fatalf("origin = %q, %v", got, err)
			}
		})
	}
}

func TestOriginRejectsIncludesAndMalformedHeaders(t *testing.T) {
	for _, header := range []string{
		`[include] # comment`,
		`[INCLUDE] ; comment`,
		"[include\t]#comment",
		"[includeIf\t\"gitdir:/tmp/path]with-bracket/\"] # comment",
		`[include "subsection"]`,
		`[includeIf.foo]`,
		`[includeIf.gitdir:/tmp/]`,
		`[include`,
		`[include] unexpected`,
		`[remote "unterminated]`,
		`[remote "origin" ]`,
		`[remote "origin"] unexpected`,
	} {
		t.Run(header, func(t *testing.T) {
			config := originConfig + header + "\npath=other\n"
			if origin, err := ParseOriginURL([]byte(config)); err == nil {
				t.Fatalf("accepted %q as origin %q", header, origin)
			}
		})
	}
}

func TestVerifyRejectsCommentedIncludeUsedByGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable:", err)
	}
	for _, comment := range []string{"# comment", "; comment"} {
		t.Run(comment, func(t *testing.T) {
			root := gitCheckout(t, originConfig+"[include] "+comment+"\npath=included\n")
			if err := os.WriteFile(filepath.Join(root, ".git", "included"), []byte("[remote \"origin\"]\nurl=https://github.com/other/repo.git\n"), 0o600); err != nil {
				t.Fatal(err)
			}
			out, err := exec.CommandContext(t.Context(), "git", "config", "--includes", "--file", filepath.Join(root, ".git", "config"), "--get", "remote.origin.url").CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != "https://github.com/other/repo.git" {
				t.Fatalf("native Git origin = %q, %v", out, err)
			}
			sel := &Selector{Origin: "github.com/furyfree-org/nimbus"}
			if err := Verify(sel, root); err == nil || !strings.Contains(err.Error(), "include directives") {
				t.Fatalf("included Git origin bypassed selector trust: %v", err)
			}
		})
	}
}

func TestVerifyRejectsDottedOriginUsedByGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git unavailable:", err)
	}
	for _, header := range []string{
		`[remote.origin]`,
		`[remote.Origin]`,
		`[Remote.ORIGIN]`,
		`[remote.origin] # comment`,
	} {
		t.Run(header, func(t *testing.T) {
			root := gitCheckout(t, originConfig+header+"\nurl=https://github.com/other/repo.git\n")
			out, err := exec.CommandContext(t.Context(), "git", "config", "--file", filepath.Join(root, ".git", "config"), "--get", "remote.origin.url").CombinedOutput()
			if err != nil || strings.TrimSpace(string(out)) != "https://github.com/other/repo.git" {
				t.Fatalf("native Git origin = %q, %v", out, err)
			}
			sel := &Selector{Origin: "github.com/furyfree-org/nimbus"}
			if err := Verify(sel, root); err == nil {
				t.Fatal("dotted Git origin bypassed selector trust")
			}
		})
	}
}

func TestOriginValueContinuations(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable:", err)
	}
	const wantURL = "https://github.com/Furyfree/nimbus.git"
	base := "[remote \"origin\"]\nurl=" + wantURL + "\n"
	for _, tc := range []struct {
		name      string
		config    string
		continued bool
	}{
		{"unrelated description", base + "[core]\ndescription=first\\\nsecond\n", true},
		{"quoted description", base + "[core]\ndescription=\"first #;\\\nsecond\"\n", true},
		{"origin URL", "[remote \"origin\"]\nurl=https://github.com/Furyfree/\\\nnimbus.git\n", true},
		{"escaped backslashes", base + "[core]\ndescription=two\\\\\n", false},
		{"quoted backslashes", base + "[core]\ndescription=\"two\\\\\"\n", false},
		{"hash comment", base + "[core]\ndescription=text # comment \\\n", false},
		{"semicolon comment", base + "[core]\ndescription=text ; comment \\\n", false},
		{"quoted comment characters", base + "[core]\ndescription=\"escaped\\\" #; text\" # comment \\\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, ending := range []struct{ name, value string }{{"LF", "\n"}, {"CRLF", "\r\n"}} {
				t.Run(ending.name, func(t *testing.T) {
					root := gitCheckout(t, strings.ReplaceAll(tc.config, "\n", ending.value))
					configPath := filepath.Join(root, ".git", "config")
					out, err := exec.CommandContext(t.Context(), git, "config", "--file", configPath, "--get", "remote.origin.url").CombinedOutput()
					if err != nil || strings.TrimSpace(string(out)) != wantURL {
						t.Fatalf("native Git origin = %q, %v", out, err)
					}
					err = Verify(&Selector{Origin: "github.com/Furyfree/nimbus"}, root)
					if tc.continued {
						if err == nil || !strings.Contains(err.Error(), "line continuations are not supported") {
							t.Fatalf("continued Git value was accepted: %v", err)
						}
					} else if err != nil {
						t.Fatalf("ordinary backslash or comment was rejected: %v", err)
					}
				})
			}
		})
	}
}

func TestOriginURLValuesMatchGit(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable:", err)
	}
	for _, locator := range []string{"https://github.com/Furyfree/nimbus.git", "git@github.com:Furyfree/nimbus.git"} {
		for _, tc := range []struct{ name, value string }{
			{"plain", locator},
			{"quoted", `"` + locator + `"`},
			{"quoted segment", strings.Replace(locator, "Furyfree", `"Furyfree"`, 1)},
			{"hash comment", locator + " # origin comment"},
			{"semicolon comment", locator + " ; origin comment"},
			{"quoted with comment", `"` + locator + `" # origin comment`},
		} {
			t.Run(locator+"/"+tc.name, func(t *testing.T) {
				root := gitCheckout(t, "[remote \"origin\"]\nurl="+tc.value+"\n")
				out, err := exec.CommandContext(t.Context(), git, "config", "--file", filepath.Join(root, ".git", "config"), "--get", "remote.origin.url").CombinedOutput()
				if err != nil || string(out) != locator+"\n" {
					t.Fatalf("native Git origin = %q, %v", out, err)
				}
				if got, err := CheckoutOrigin(root); err != nil || got != locator {
					t.Fatalf("checkout origin = %q, %v; want %q", got, err, locator)
				}
				if err := Verify(&Selector{Origin: "github.com/Furyfree/nimbus"}, root); err != nil {
					t.Fatalf("equivalent origin was rejected: %v", err)
				}
			})
		}
	}
}

func TestOriginValueEscapesAndWhitespaceMatchGit(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable:", err)
	}
	for _, tc := range []struct{ name, value, want string }{
		{"quoted escapes", `"first\n\t\b\\\"last"`, "first\n\t\b\\\"last"},
		{"unquoted escapes", `first\n\t\b\\\"last`, "first\n\t\b\\\"last"},
		{"quoted comment characters", `"first #; last" # comment`, "first #; last"},
		{"quoted whitespace", `  " padded "  `, " padded "},
		{"trailing empty segment", `first   ""`, "first   "},
		{"leading empty segment", `""  last`, "last"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := gitCheckout(t, "[remote \"origin\"]\nurl="+tc.value+"\n")
			out, err := exec.CommandContext(t.Context(), git, "config", "--file", filepath.Join(root, ".git", "config"), "--get", "remote.origin.url").CombinedOutput()
			if err != nil || string(out) != tc.want+"\n" {
				t.Fatalf("native Git value = %q, %v", out, err)
			}
			if got, err := CheckoutOrigin(root); err != nil || got != tc.want {
				t.Fatalf("checkout value = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestOriginRejectsInvalidValueEncoding(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable:", err)
	}
	for _, value := range []string{`"unterminated`, `"bad\q"`, `bad\q`, `bad\r`, `bad\x20`, `bad\040`, `bad\#`} {
		for _, prefix := range []string{"[remote \"origin\"]\nurl=", "[core]\ndescription="} {
			t.Run(prefix+value, func(t *testing.T) {
				root := gitCheckout(t, prefix+value+"\n"+originConfig)
				config := filepath.Join(root, ".git", "config")
				if out, err := exec.CommandContext(t.Context(), git, "config", "--file", config, "--get", "remote.origin.url").CombinedOutput(); err == nil {
					t.Fatalf("native Git accepted invalid encoding: %q", out)
				}
				if _, err := CheckoutOrigin(root); err == nil || !strings.Contains(err.Error(), config) || !strings.Contains(err.Error(), "line 2") {
					t.Fatalf("invalid encoding lacked file and line context: %v", err)
				}
			})
		}
	}
}

func TestEmptyOriginDoesNotHideDuplicateURL(t *testing.T) {
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git unavailable:", err)
	}
	const locator = "https://github.com/Furyfree/nimbus.git"
	for _, entry := range []string{"url", " \tURL \t", "url=", `url=""`, "url= # no value", "url= ; no value"} {
		t.Run(entry, func(t *testing.T) {
			config := "[remote \"origin\"]\n" + entry + "\nurl=" + locator + "\n"
			root := gitCheckout(t, config)
			out, err := exec.CommandContext(t.Context(), git, "config", "--file", filepath.Join(root, ".git", "config"), "--get-all", "remote.origin.url").CombinedOutput()
			if err != nil || string(out) != "\n"+locator+"\n" {
				t.Fatalf("native Git origins = %q, %v", out, err)
			}
			if err := Verify(&Selector{Origin: "github.com/Furyfree/nimbus"}, root); err == nil || !strings.Contains(err.Error(), "more than once") {
				t.Fatalf("empty first URL %q hid a duplicate: %v", entry, err)
			}
		})
	}
}
