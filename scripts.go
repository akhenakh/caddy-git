package git

import (
	"fmt"
	"os"
	"sync"

	"github.com/abiosoft/caddy-git/gitos"
)

var (
	// gitBinary holds the absolute path to git executable
	gitBinary string

	// shell holds the shell to be used. Either sh or bash.
	shell string

	// initMutex prevents parallel attempt to validate
	// git requirements.
	initMutex = sync.Mutex{}
)

// Init validates git installation, locates the git executable
// binary in PATH and check for available shell to use.
func Init() error {
	// prevent concurrent call
	initMutex.Lock()
	defer initMutex.Unlock()

	// if validation has been done before and binary located in
	// PATH, return.
	if gitBinary != "" {
		return nil
	}

	// locate git binary in path
	var err error
	if gitBinary, err = gos.LookPath("git"); err != nil {
		return fmt.Errorf("git middleware requires git installed. Cannot find git binary in PATH")
	}

	return nil
}

// writeScriptFile writes content to a temporary file.
// It changes the temporary file mode to executable and
// closes it to prepare it for execution.
func writeScriptFile(content []byte) (file gitos.File, err error) {
	if file, err = gos.TempFile("", "caddy"); err != nil {
		return nil, err
	}
	if _, err = file.Write(content); err != nil {
		return nil, err
	}
	if err = file.Chmod(os.FileMode(0755)); err != nil {
		return nil, err
	}
	return file, file.Close()
}
