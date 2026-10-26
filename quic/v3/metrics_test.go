package v3_test

import v3 "github.com/cloudflare/cloudflared/quic/v3"

type noopMetrics struct{}

func (noopMetrics) IncrementFlows(connIndex uint8)                              {}
func (noopMetrics) DecrementFlows(connIndex uint8)                              {}
func (noopMetrics) FailedFlow(connIndex uint8)                                  {}
func (noopMetrics) PayloadTooLarge(connIndex uint8)                             {}
func (noopMetrics) RetryFlowResponse(connIndex uint8)                           {}
func (noopMetrics) MigrateFlow(connIndex uint8)                                 {}
func (noopMetrics) UnsupportedRemoteCommand(connIndex uint8, command string)    {}
func (noopMetrics) DroppedUDPDatagram(connIndex uint8, reason v3.DroppedReason) {}
func (noopMetrics) DroppedICMPPackets(connIndex uint8, reason v3.DroppedReason) {}
