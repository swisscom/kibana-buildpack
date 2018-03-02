package util

import (
	"regexp"
	"os"
	"path/filepath"
)

func TrimLines(text string) string {
	re := regexp.MustCompile("(?m)^(\\s)*")
	return re.ReplaceAllString(text, "")
}

func RemoveAllContents(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	names, err := d.Readdirnames(-1)
	if err != nil {
		return err
	}
	for _, name := range names {
		err = os.RemoveAll(filepath.Join(dir, name))
		if err != nil {
			return err
		}
	}
	return nil
}