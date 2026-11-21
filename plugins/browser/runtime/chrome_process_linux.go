//go:build linux

package runtime

import (
	"io"
	"os"
	"os/exec"
)

func startChromeProcess(spec Spec, environment []string, output io.Writer) (*child, *cdpClient, error) {
	chrome := exec.Command(ChromeBinary, chromeArguments(spec)...)
	chrome.Env = environment
	chrome.Stdout, chrome.Stderr = io.Discard, output
	if spec.BrowserMode() == ModeBrowse {
		if err := chrome.Start(); err != nil {
			return nil, nil, err
		}
		return observeChild(chrome, childChrome), nil, nil
	}
	readInput, writeInput, err := os.Pipe()
	if err != nil {
		return nil, nil, err
	}
	readOutput, writeOutput, err := os.Pipe()
	if err != nil {
		readInput.Close()
		writeInput.Close()
		return nil, nil, err
	}
	chrome.ExtraFiles = []*os.File{readInput, writeOutput}
	if err := chrome.Start(); err != nil {
		readInput.Close()
		writeInput.Close()
		readOutput.Close()
		writeOutput.Close()
		return nil, nil, err
	}
	readInput.Close()
	writeOutput.Close()
	return observeChild(chrome, childChrome), newCDP(writeInput, readOutput), nil
}
