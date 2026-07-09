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
		// 带缓冲的 channel: getWarning 使用 select/default 非阻塞读取, 若在网络请求完成前被读取,
		// 无缓冲 channel 会导致发送方 goroutine 永久阻塞, 造成泄漏. 缓冲为 1 保证发送方总能完成发送并 close.
		warningChan: make(chan string, 1),
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
