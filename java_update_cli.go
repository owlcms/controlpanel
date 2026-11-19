package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"

	"controlpanel/shared"
)

func checkJavaAtHeadlessStartup(in *bufio.Reader, interactive bool, out, errOut io.Writer) {
	installed, err := shared.InspectJava("jdk-25")
	installedVersion := ""
	needsUpdate := errors.Is(err, shared.ErrJavaNotInstalled)
	if installed != nil {
		installedVersion = installed.Version
		meets, compareErr := shared.JavaVersionAtLeast(installedVersion, shared.MinimumJavaVersion)
		if compareErr != nil {
			log.Printf("Cannot compare installed Java %s with minimum %s: %v",
				installedVersion, shared.MinimumJavaVersion, compareErr)
		} else {
			needsUpdate = !meets
		}
	} else if err != nil && !needsUpdate {
		log.Printf("Cannot inspect installed Java: %v", err)
	}
	if !needsUpdate {
		return
	}

	message := fmt.Sprintf("Java security update required: installed %s, required %s.",
		displayJavaVersion(installedVersion), shared.MinimumJavaVersion)
	fmt.Fprintln(errOut, message)
	log.Print(message)
	if !interactive {
		fmt.Fprintln(errOut, "Java was not updated; use the GUI or 'java update' interactively.")
		return
	}

	fmt.Fprint(out, "Download and install the latest Java 25 now? [Y/n] ")
	answer, readErr := in.ReadString('\n')
	if readErr != nil && len(answer) == 0 {
		fmt.Fprintf(errOut, "Cannot read Java update choice: %v\n", readErr)
		return
	}
	answer = strings.TrimSpace(strings.ToLower(answer))
	if answer != "" && answer != "y" && answer != "yes" {
		return
	}
	if _, err := updateJavaHeadless(false, out); err != nil {
		fmt.Fprintf(errOut, "Java update was not installed: %v\n", err)
	}
}

func executeREPLJava(args []string, out io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: java status | java update")
	}
	switch args[0] {
	case "status":
		installed, err := shared.InspectJava("jdk-25")
		if errors.Is(err, shared.ErrJavaNotInstalled) {
			fmt.Fprintf(out, "installed: none\nminimum:   %s\n", shared.MinimumJavaVersion)
			return nil
		}
		if err != nil {
			return err
		}
		meets, err := shared.JavaVersionAtLeast(installed.Version, shared.MinimumJavaVersion)
		if err != nil {
			return err
		}
		fmt.Fprintf(out, "installed: %s\nminimum:   %s\naccepted:   %t\npath:       %s\n",
			installed.Version, shared.MinimumJavaVersion, meets, installed.Home)
		return nil
	case "update":
		result, err := updateJavaHeadless(true, out)
		if err != nil {
			return err
		}
		if result.Updated {
			fmt.Fprintf(out, "Java updated from %s to %s\n",
				displayJavaVersion(result.Previous), result.Current)
		} else {
			fmt.Fprintf(out, "Java %s is already current\n", result.Current)
		}
		return nil
	default:
		return fmt.Errorf("usage: java status | java update")
	}
}

func updateJavaHeadless(latest bool, out io.Writer) (*shared.JavaUpdateResult, error) {
	cancel := make(chan bool)
	interrupt := make(chan os.Signal, 1)
	signal.Notify(interrupt, os.Interrupt)
	done := make(chan struct{})
	var cancelOnce sync.Once
	go func() {
		select {
		case <-interrupt:
			cancelOnce.Do(func() { close(cancel) })
		case <-done:
		}
	}()
	defer func() {
		signal.Stop(interrupt)
		close(done)
	}()

	progress := func(downloaded, total int64) {
		if total > 0 {
			fmt.Fprintf(out, "\rDownloading Java... %.1f%%", float64(downloaded)/float64(total)*100)
		}
	}
	var result *shared.JavaUpdateResult
	var updateErr error
	_, err := shared.SerializeJavaUpdate(func() (*shared.JavaUpdateResult, error) {
		if latest {
			result, updateErr = shared.UpdateToLatestJava(progress, cancel)
		} else {
			result, updateErr = shared.EnsureMinimumJava(progress, cancel)
		}
		return result, updateErr
	})
	fmt.Fprintln(out)
	if err != nil {
		return nil, err
	}
	return result, nil
}

func ensureJavaForHeadlessLaunch(out io.Writer) error {
	installed, err := shared.InspectJava("jdk-25")
	if err == nil {
		meets, compareErr := shared.JavaVersionAtLeast(installed.Version, shared.MinimumJavaVersion)
		if compareErr != nil {
			return compareErr
		}
		if !meets {
			fmt.Fprintf(out, "warning: installed Java %s is below required %s; use 'java update'\n",
				installed.Version, shared.MinimumJavaVersion)
		}
		return nil
	}
	if !errors.Is(err, shared.ErrJavaNotInstalled) {
		return err
	}
	result, err := updateJavaHeadless(false, out)
	if errors.Is(err, context.Canceled) {
		return fmt.Errorf("Java installation cancelled")
	}
	if err != nil {
		return fmt.Errorf("installing required Java: %w", err)
	}
	if result == nil || result.Current == "" {
		return fmt.Errorf("Java installation did not produce a runtime")
	}
	return nil
}
