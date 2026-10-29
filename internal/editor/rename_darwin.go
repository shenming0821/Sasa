package editor

import "golang.org/x/sys/unix"

func Rename(path, target string) error {
	return unix.RenamexNp(path, target, unix.RENAME_EXCL)
}
