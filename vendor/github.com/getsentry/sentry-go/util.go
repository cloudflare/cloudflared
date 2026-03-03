package sentry

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime/debug"
	"strings"
	"time"

	"github.com/getsentry/sentry-go/internal/debuglog"
	"github.com/getsentry/sentry-go/internal/protocol"
	exec "golang.org/x/sys/execabs"
)

func uuid() string {
	return protocol.GenerateEventID()
}

func fileExists(fileName string) bool {
	_, err := os.Stat(fileName)
	return err == nil
}

// monotonicTimeSince replaces uses of time.Now() to take into account the
// monotonic clock reading stored in start, such that duration = end - start is
// unaffected by changes in the system wall clock.
func monotonicTimeSince(start time.Time) (end time.Time) {
	return start.Add(time.Since(start))
}

// nolint: unused
func prettyPrint(data interface{}) {
	dbg, _ := json.MarshalIndent(data, "", "  ")
	fmt.Println(string(dbg))
}

// defaultRelease attempts to guess a default release for the currently running
// program.
func defaultRelease() (release string) {
	// Return first non-empty environment variable known to hold release info, if any.
	envs := []string{
		"SENTRY_RELEASE",
		"HEROKU_SLUG_COMMIT",
		"SOURCE_VERSION",
		"CODEBUILD_RESOLVED_SOURCE_VERSION",
		"CIRCLE_SHA1",
		"GAE_DEPLOYMENT_ID",
		"GITHUB_SHA",             // GitHub Actions - https://help.github.com/en/actions
		"COMMIT_REF",             // Netlify - https://docs.netlify.com/
		"VERCEL_GIT_COMMIT_SHA",  // Vercel - https://vercel.com/
		"ZEIT_GITHUB_COMMIT_SHA", // Zeit (now known as Vercel)
		"ZEIT_GITLAB_COMMIT_SHA",
		"ZEIT_BITBUCKET_COMMIT_SHA",
	}
	for _, e := range envs {
		if release = os.Getenv(e); release != "" {
			debuglog.Printf("Using release from environment variable %s: %s", e, release)
			return release
		}
	}

	if info, ok := debug.ReadBuildInfo(); ok {
		buildInfoVcsRevision := revisionFromBuildInfo(info)
		if len(buildInfoVcsRevision) > 0 {
			return buildInfoVcsRevision
		}
	}

	// Derive a version string from Git. Example outputs:
	// 	v1.0.1-0-g9de4
	// 	v2.0-8-g77df-dirty
	// 	4f72d7
	if _, err := exec.LookPath("git"); err == nil {
		cmd := exec.Command("git", "describe", "--long", "--always", "--dirty")
		b, err := cmd.Output()
		if err != nil {
			// Either Git is not available or the current directory is not a
			// Git repository.
			var s strings.Builder
			fmt.Fprintf(&s, "Release detection failed: %v", err)
			if err, ok := err.(*exec.ExitError); ok && len(err.Stderr) > 0 {
				fmt.Fprintf(&s, ": %s", err.Stderr)
			}
			debuglog.Print(s.String())
		} else {
			release = strings.TrimSpace(string(b))
			debuglog.Printf("Using release from Git: %s", release)
			return release
		}
	}

	debuglog.Print("Some Sentry features will not be available. See https://docs.sentry.io/product/releases/.")
	debuglog.Print("To stop seeing this message, pass a Release to sentry.Init or set the SENTRY_RELEASE environment variable.")
	return ""
}

func revisionFromBuildInfo(info *debug.BuildInfo) string {
	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" && setting.Value != "" {
			debuglog.Printf("Using release from debug info: %s", setting.Value)
			return setting.Value
		}
	}

	return ""
}

func Pointer[T any](v T) *T {
	return &v
}

// eventIdentifier returns a human-readable identifier for the event to be used in log messages.
// Format: "<description> [<event-id>]".
func eventIdentifier(event *Event) string {
	var description string
	switch event.Type {
	case errorType:
		description = "error"
	case transactionType:
		description = "transaction"
	case checkInType:
		description = "check-in"
	case logEvent.Type:
		description = fmt.Sprintf("%d log events", len(event.Logs))
	case traceMetricEvent.Type:
		description = fmt.Sprintf("%d metric events", len(event.Metrics))
	default:
		description = fmt.Sprintf("%s event", event.Type)
	}
	return fmt.Sprintf("%s [%s]", description, event.EventID)
}
