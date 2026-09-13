package postinstall

import (
	"bytes"
	"encoding/json"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Furyfree/nimbus/internal/native/nativetest"
)

func pictureFixture(t *testing.T) (Inputs, *nativetest.FakeSource, string) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	in, src := fixture("accountsservice")
	var img bytes.Buffer
	if err := jpeg.Encode(&img, image.NewRGBA(image.Rect(0, 0, 2, 2)), nil); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".config/noctalia/assets/profile-picture.jpg")
	src.Files[path] = img.Bytes()
	src.Paths["/usr/bin/busctl"] = "/usr/bin/busctl"
	src.Commands["id -u"] = []byte("1000\n")
	return in, src, path
}

func picturePropertyKey(property string) string {
	return "/usr/bin/busctl --system --auto-start=no --allow-interactive-authorization=no --json=short get-property org.freedesktop.Accounts /org/freedesktop/Accounts/User1000 org.freedesktop.Accounts.User " + property
}

func TestAccountPictureReadiness(t *testing.T) {
	for _, mode := range []string{"unset", "missing old file", "different", "matching", "changed source", "missing source", "invalid source", "truncated source", "large source", "unknown service", "wrong user", "invalid property", "null property", "unreadable old file", "missing receipt", "unknown packages", "root", "invalid uid", "missing busctl", "unselected"} {
		t.Run(mode, func(t *testing.T) {
			in, src, path := pictureFixture(t)
			src.Commands[picturePropertyKey("UserName")] = []byte(`{"type":"s","data":"tester"}`)
			src.Commands[picturePropertyKey("IconFile")] = []byte(`{"type":"s","data":"/var/lib/AccountsService/icons/tester"}`)
			expected := Pending
			switch mode {
			case "unset":
				src.Commands[picturePropertyKey("IconFile")] = []byte(`{"type":"s","data":""}`)
			case "missing old file":
			case "different":
				src.Files["/var/lib/AccountsService/icons/tester"] = []byte("old image")
			case "matching", "changed source":
				src.Files["/var/lib/AccountsService/icons/tester"] = slices.Clone(src.Files[path])
				if mode == "matching" {
					expected = Complete
				} else {
					src.Files[path] = append(slices.Clone(src.Files[path]), []byte("new version")...)
				}
			case "missing source":
				delete(src.Files, path)
				expected = Blocked
			case "invalid source":
				src.Files[path] = []byte("not a jpeg")
				expected = Blocked
			case "truncated source":
				src.Files[path] = src.Files[path][:len(src.Files[path])-10]
				expected = Blocked
			case "large source":
				src.Files[path] = append(src.Files[path], make([]byte, 1024*1024)...)
				expected = Blocked
			case "unknown service":
				src.Failures[picturePropertyKey("UserName")] = "not running"
				expected = Unknown
			case "wrong user":
				src.Commands[picturePropertyKey("UserName")] = []byte(`{"type":"s","data":"root"}`)
				expected = Unknown
			case "invalid property":
				src.Commands[picturePropertyKey("IconFile")] = []byte(`{}`)
				expected = Unknown
			case "null property":
				src.Commands[picturePropertyKey("IconFile")] = []byte(`{"type":"s","data":null}`)
				expected = Unknown
			case "unreadable old file":
				src.Dirs = map[string][]string{"/var/lib/AccountsService/icons/tester": {}}
				expected = Unknown
			case "missing receipt":
				clear(in.Applied.Receipts)
				expected = Blocked
			case "unknown packages":
				in.Facts.Packages.Error = "unavailable"
				expected = Unknown
			case "root":
				in.Facts.User.Value.Name = "root"
				expected = Blocked
			case "invalid uid":
				src.Commands["id -u"] = []byte("0\n")
				expected = Blocked
			case "missing busctl":
				delete(src.Paths, "/usr/bin/busctl")
				expected = Blocked
			case "unselected":
				in.Resolved.Packages = nil
			}
			guard := &readGuard{FakeSource: src}
			tasks := Inspect(guard, in)
			if mode == "unselected" {
				if len(tasks) != 0 || len(guard.commands) != 0 || len(guard.files) != 0 {
					t.Fatal("unselected account inspected")
				}
				return
			}
			task := findTask(t, tasks, "account-picture")
			if task.Status != expected || (task.Action != nil) != (expected == Pending) {
				t.Fatalf("unexpected task: %+v", task)
			}
			for _, command := range guard.commands {
				if command != "id -u" && command != picturePropertyKey("UserName") && command != picturePropertyKey("IconFile") {
					t.Fatalf("unexpected command: %s", command)
				}
			}
			data, _ := json.Marshal(task)
			if strings.Contains(string(data), "old image") || bytes.Contains(data, src.Files[path]) && len(src.Files[path]) > 0 {
				t.Fatal("image bytes retained")
			}
			if task.Action != nil && task.Action.Argv[len(task.Action.Argv)-1] != path {
				t.Fatal("wrong source path")
			}
		})
	}
}

func TestAccountPictureApprovalTracksContents(t *testing.T) {
	in, src, path := pictureFixture(t)
	src.Commands[picturePropertyKey("UserName")] = []byte(`{"type":"s","data":"tester"}`)
	src.Commands[picturePropertyKey("IconFile")] = []byte(`{"type":"s","data":"/var/lib/AccountsService/icons/tester"}`)
	src.Files["/var/lib/AccountsService/icons/tester"] = []byte("before")
	first := findTask(t, Inspect(src, in), "account-picture")
	src.Files["/var/lib/AccountsService/icons/tester"] = []byte("after")
	second := findTask(t, Inspect(src, in), "account-picture")
	if first.Action.Picture.CurrentSHA256 == second.Action.Picture.CurrentSHA256 {
		t.Fatal("current content not bound to approval")
	}
	src.Files[path] = append(slices.Clone(src.Files[path]), []byte("source changed")...)
	third := findTask(t, Inspect(src, in), "account-picture")
	if second.Action.Picture.SourceSHA256 == third.Action.Picture.SourceSHA256 {
		t.Fatal("source content not bound to approval")
	}
	if _, err := os.Stat(filepath.Join(os.Getenv("HOME"), ".config")); !os.IsNotExist(err) {
		t.Fatal("inspection wrote user files")
	}
}
