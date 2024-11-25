package diagnostic

import "time"

const (
	defaultCollectorTimeout    = time.Second * 10 // This const define the timeout value of a collector operation.
	collectorField             = "collector"      // used for logging purposes
	systemCollectorName        = "system"         // used for logging purposes
	tunnelStateCollectorName   = "tunnelState"    // used for logging purposes
	configurationCollectorName = "configuration"  // used for logging purposes
	configurationKeyUid        = "uid"
)
