package metrics

import (
  "time"
  "github.com/prometheus/client_golang/prometheus"
)

// Timer assumes the metrics is partitioned by one label
type Timer struct {
  startTime   map[string]time.Time
  metrics     *prometheus.HistogramVec
  measureUnit time.Duration
  labelKey    string
}

func NewTimer(metrics *prometheus.HistogramVec, unit time.Duration, labelKey string) *Timer {
  return &Timer{
    startTime:   make(map[string]time.Time),
    measureUnit: unit,
    metrics:     metrics,
    labelKey:    labelKey,
  }
}

func (i *Timer) Start(labelVal string) {
  i.startTime[labelVal] = time.Now()
}

func (i *Timer) End(labelVal string) time.Duration {
  if start, ok := i.startTime[labelVal]; ok {
    return Latency(start, time.Now())
  }
  return 0
}

func (i *Timer) Observe(measurement time.Duration, labelVal string) {
  metricsLabels := prometheus.Labels{i.labelKey: labelVal}
  i.metrics.With(metricsLabels).Observe(float64(measurement / i.measureUnit))
}

func (i *Timer) EndAndObserve(labelVal string) {
  i.Observe(i.End(labelVal), labelVal)
}

func Latency(startTime, endTime time.Time) time.Duration {
  return endTime.Sub(startTime)
}
