//go:build darwin

// Command source-write-probe calibrates the verification sandbox on disposable
// files only. It is never included in a Skuggsja release binary.
package main

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

var fixtureNames = []string{"append", "truncate", "rename", "remove", "chmod", "chtime", "link-source", "mmap", "replace-source", "replace-target"}

func main() {
	if len(os.Args) == 3 && os.Args[1] == "prepare" {
		if err := prepare(os.Args[2]); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) != 4 || (os.Args[1] != "unconfined" && os.Args[1] != "denied") {
		fmt.Fprintln(os.Stderr, "usage: source-write-probe prepare EMPTY_CANARY_DIR | {unconfined|denied} CANARY_DIR WRITABLE_DIR")
		os.Exit(2)
	}
	if err := probe(os.Args[1], os.Args[2], os.Args[3]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func prepare(root string) error {
	entries, err := os.ReadDir(root)
	if err != nil || len(entries) != 0 {
		return errors.New("canary preparation requires an existing empty directory")
	}
	for _, name := range fixtureNames {
		if err := os.WriteFile(filepath.Join(root, name), make([]byte, 4096), 0o600); err != nil {
			return fmt.Errorf("prepare disposable canary: %w", err)
		}
	}
	return nil
}

func probe(mode, root, writable string) error {
	before, err := snapshot(root)
	if err != nil {
		return err
	}
	path := func(name string) string { return filepath.Join(root, name) }
	operations := []struct {
		name string
		run  func() error
	}{
		{"create", func() error { return writeFile(path("absent-file"), os.O_WRONLY|os.O_CREATE|os.O_EXCL) }},
		{"append", func() error { return writeFile(path("append"), os.O_WRONLY|os.O_APPEND) }},
		{"truncate", func() error { return os.Truncate(path("truncate"), 0) }},
		{"rename", func() error { return os.Rename(path("rename"), path("renamed")) }},
		{"replace", func() error { return os.Rename(path("replace-source"), path("replace-target")) }},
		{"remove", func() error { return os.Remove(path("remove")) }},
		{"mkdir", func() error { return os.Mkdir(path("absent-directory"), 0o700) }},
		{"chmod", func() error { return os.Chmod(path("chmod"), 0o400) }},
		{"chtime", func() error { return os.Chtimes(path("chtime"), time.Unix(123, 0), time.Unix(123, 0)) }},
		{"hardlink", func() error { return os.Link(path("link-source"), path("hardlink")) }},
		{"symlink", func() error { return os.Symlink(path("link-source"), path("symlink")) }},
		{"shared_mapping", func() error { return sharedMapping(path("mmap")) }},
	}
	passed := 0
	for _, operation := range operations {
		err := operation.run()
		ok := err == nil
		if mode == "denied" {
			ok = errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES)
		}
		fmt.Printf("source_write_control mode=%s operation=%s passed=%t\n", mode, operation.name, ok)
		if ok {
			passed++
		}
	}
	insidePath := filepath.Join(writable, "positive-write")
	if err := writeFile(insidePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY); err != nil {
		return fmt.Errorf("writable-workspace positive control failed: %w", err)
	}
	if err := os.Remove(insidePath); err != nil {
		return fmt.Errorf("writable-workspace cleanup control failed: %w", err)
	}
	fmt.Println("source_write_control operation=writable_workspace passed=true")
	if mode == "denied" {
		after, err := snapshot(root)
		if err != nil {
			return err
		}
		if !reflect.DeepEqual(before, after) {
			return errors.New("denied canary content, metadata, or directory membership changed")
		}
		fmt.Println("source_write_control operation=outside_canary_unchanged passed=true")
	}
	fmt.Printf("source_write_controls mode=%s passed=%d total=%d\n", mode, passed, len(operations))
	if passed != len(operations) {
		return errors.New("source-write calibration failed")
	}
	return nil
}

func writeFile(path string, flags int) error {
	file, err := os.OpenFile(path, flags, 0o600)
	if err != nil {
		return err
	}
	_, writeErr := file.Write([]byte("disposable calibration\n"))
	return errors.Join(writeErr, file.Close())
}

func sharedMapping(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	data, err := unix.Mmap(int(file.Fd()), 0, 4096, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_SHARED)
	if err != nil {
		return errors.Join(err, file.Close())
	}
	data[0] = 1
	return errors.Join(unix.Msync(data, unix.MS_SYNC), unix.Munmap(data), file.Close())
}

type fileState struct {
	Mode    fs.FileMode
	ModTime int64
	Size    int64
	Hash    [sha256.Size]byte
}

func snapshot(root string) (map[string]fileState, error) {
	states := make(map[string]fileState)
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		state := fileState{Mode: info.Mode(), ModTime: info.ModTime().UnixNano(), Size: info.Size()}
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			state.Hash = sha256.Sum256(data)
		}
		states[path] = state
		return nil
	})
	return states, err
}
