package v3_test

type noopMetrics struct{}

func (noopMetrics) IncrementFlows()    {}
func (noopMetrics) DecrementFlows()    {}
func (noopMetrics) PayloadTooLarge()   {}
func (noopMetrics) RetryFlowResponse() {}
func (noopMetrics) MigrateFlow()       {}
