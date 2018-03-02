package golang

import (
	"fmt"
)

func ReleaseYAML(startCmd string) string {
	release := `---
default_process_types:
    web: %s
`
	return fmt.Sprintf(release, startCmd)
}

func GoScript() string {
	return "PATH=$PATH:$HOME/bin\n"
}

