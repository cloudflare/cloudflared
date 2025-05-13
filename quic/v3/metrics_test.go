package v3_test

type noopMetrics struct{}

func (noopMetrics) IncrementFlows(connIndex uint8)                           {}
func (noopMetrics) DecrementFlows(connIndex uint8)                           {}
func (noopMetrics) PayloadTooLarge(connIndex uint8)                          {}
func (noopMetrics) RetryFlowResponse(connIndex uint8)                        {}
func (noopMetrics) MigrateFlow(connIndex uint8)                              {}
func (noopMetrics) UnsupportedRemoteCommand(connIndex uint8, command string) {}
