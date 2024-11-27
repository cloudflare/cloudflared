package diagnostic

import "time"

const (
	defaultCollectorTimeout    = time.Second * 10       // This const define the timeout value of a collector operation.
	collectorField             = "collector"            // used for logging purposes
	systemCollectorName        = "system"               // used for logging purposes
	tunnelStateCollectorName   = "tunnelState"          // used for logging purposes
	configurationCollectorName = "configuration"        // used for logging purposes
	defaultTimeout             = 15 * time.Second       // timeout for the collectors
	twoWeeksOffset             = -14 * 24 * time.Hour   // maximum offset for the logs
	logFilename                = "cloudflared_logs.txt" // name of the output log file
	configurationKeyUID        = "uid"                  // Key used to set and get the UID value from the configuration map
	tailMaxNumberOfLines       = "10000"                // maximum number of log lines from a virtual runtime (docker or kubernetes)
)
