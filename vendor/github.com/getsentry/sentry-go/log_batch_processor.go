package sentry

import (
	"time"
)

// logBatchProcessor batches logs and sends them to Sentry.
type logBatchProcessor struct {
	*batchProcessor[Log]
}

func newLogBatchProcessor(client *Client) *logBatchProcessor {
	return &logBatchProcessor{
		batchProcessor: newBatchProcessor(func(items []Log) {
			if len(items) == 0 {
				return
			}

			event := NewEvent()
			event.Timestamp = time.Now()
			event.EventID = EventID(uuid())
			event.Type = logEvent.Type
			event.Logs = items

			client.Transport.SendEvent(event)
		}),
	}
}

func (p *logBatchProcessor) Send(log *Log) bool {
	return p.batchProcessor.Send(*log)
}
