package microvm

import "os/exec"

func execLookPathStd(file string) (string, error) {
	return exec.LookPath(file)
}
