package origins

type noopMetrics struct{}

func (noopMetrics) IncrementDNSUDPRequests() {}
func (noopMetrics) IncrementDNSTCPRequests() {}
