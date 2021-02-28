package updater

import (
	"github.com/rs/zerolog"
	"github.com/urfave/cli/v2"
)

type VersionWarningChecker struct {
	warningChan chan string
}

func StartWarningCheck(c *cli.Context) VersionWarningChecker {
	checker := VersionWarningChecker{
		warningChan: make(chan string),
	}

	go func() {
		options := updateOptions{
			updateDisabled:  true,
			isBeta:          c.Bool("beta"),
			isStaging:       c.Bool("staging"),
			isForced:        false,
			intendedVersion: "",
		}
		checkResult, err := CheckForUpdate(options)
		if err == nil {
			checker.warningChan <- checkResult.UserMessage()
		}
		close(checker.warningChan)
	}()

	return checker
}

func (checker VersionWarningChecker) getWarning() string {
	select {
	case message := <-checker.warningChan:
		return message
	default:
		// No feedback on time, we don't wait for it, since this is best-effort.
		return ""
	}
}

func (checker VersionWarningChecker) LogWarningIfAny(log *zerolog.Logger) {
	if warning := checker.getWarning(); warning != "" {
		log.Warn().Msg(warning)
	}
}
