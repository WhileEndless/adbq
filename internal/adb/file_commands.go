package adb

import (
	"context"
	"path"
)

// The remote commands behind the Files screen. Kept as functions so the
// listing, the deletion and the preview the user reads before confirming come
// from the same source (CLAUDE.md §4.1).
//
// Remote paths are quoted by the same rule as host arguments: the device shell
// is a POSIX shell too, and one rule keeps a pasted command identical to the
// one that ran.

// lsRemote is the preferred listing: almost-all, with a trailing slash marking
// directories.
func lsRemote(p string) string { return "ls -lAp " + quoteArg(p) }

// lsFallbackRemote is the portable form ListDir retries with when a toolbox `ls`
// rejects -A/-p.
func lsFallbackRemote(p string) string { return "ls -la " + quoteArg(p) }

func rmRemote(p string, recursive bool) string {
	flag := "-f"
	if recursive {
		flag = "-rf"
	}
	return "rm " + flag + " " + quoteArg(p)
}

func mkdirRemote(p string) string { return "mkdir -p " + quoteArg(p) }

func chmodRemote(p, mode string) string { return "chmod " + mode + " " + quoteArg(p) }

func chownRemote(p, owner string) string { return "chown " + owner + " " + quoteArg(p) }

func mvRemote(src, dst string) string { return "mv " + quoteArg(src) + " " + quoteArg(dst) }

// FileCommandRequest describes what the Files screen is about to do: which
// directory it is looking at, which entry is selected, whether the Root toggle
// is on, and the optional mode/owner a push would apply afterwards.
type FileCommandRequest struct {
	Dir    string `json:"dir"`
	Name   string `json:"name"`
	IsDir  bool   `json:"isDir"`
	AsRoot bool   `json:"asRoot"`
	Mode   string `json:"mode"`
	Owner  string `json:"owner"`
}

// FileCommands is one command list per action the screen offers.
type FileCommands struct {
	Path   string   `json:"path"` // the selected entry's full remote path, "" when none
	List   []string `json:"list"`
	Mkdir  []string `json:"mkdir"` // for a new child of Dir; the name is the user's to fill in
	Push   []string `json:"push"`
	Pull   []string `json:"pull"`
	Delete []string `json:"delete"`
}

// pushPlaceholder stands in for the file the picker has not been shown for yet.
// It is deliberately unmistakable: a preview that looked like a real path would
// invite pasting a command that copies the wrong file.
const pushPlaceholder = "<local-file>"

// newFolderPlaceholder is the same idea for a folder that has no name yet.
const newFolderPlaceholder = "<new-folder>"

// FileCommandsFor builds the Files screen's previews. render carries the `su`
// form for the Root toggle; host-side transfers never need it, because adb pull
// and adb push run on this computer.
func FileCommandsFor(serial string, req FileCommandRequest, render CommandRenderer) FileCommands {
	dir := req.Dir
	if dir == "" {
		dir = "/"
	}
	fc := FileCommands{
		List:  []string{render(lsRemote(dir), req.AsRoot)},
		Mkdir: []string{render(mkdirRemote(path.Join(dir, newFolderPlaceholder)), req.AsRoot)},
	}
	push := []string{DeviceCommandText(serial, "push", pushPlaceholder, dir)}
	pushed := path.Join(dir, path.Base(pushPlaceholder))
	if req.Mode != "" {
		push = append(push, render(chmodRemote(pushed, req.Mode), req.AsRoot))
	}
	if req.Owner != "" {
		push = append(push, render(chownRemote(pushed, req.Owner), req.AsRoot))
	}
	fc.Push = push

	if req.Name == "" {
		return fc
	}
	target := path.Join(dir, req.Name)
	fc.Path = target
	fc.Delete = []string{render(rmRemote(target, req.IsDir), req.AsRoot)}
	if !req.IsDir {
		fc.Pull = []string{DeviceCommandText(serial, "pull", target, req.Name)}
	}
	return fc
}

// FileCommandsFor is the device-aware entry point: the root steps carry the `su`
// form this device accepted.
func (c *Client) FileCommandsFor(ctx context.Context, serial string, req FileCommandRequest) FileCommands {
	return FileCommandsFor(serial, req, c.Renderer(ctx, serial))
}
