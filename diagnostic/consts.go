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

	// Endpoints used by the diagnostic HTTP Client.
	cliConfigurationEndpoint    = "/diag/configuration"
	tunnelStateEndpoint         = "/diag/tunnel"
	systemInformationEndpoint   = "/diag/system"
	memoryDumpEndpoint          = "debug/pprof/heap"
	goroutineDumpEndpoint       = "debug/pprof/goroutine"
	metricsEndpoint             = "metrics"
	tunnelConfigurationEndpoint = "/config"
	// Base for filenames of the diagnostic procedure
	systemInformationBaseName = "systeminformation.json"
	metricsBaseName           = "metrics.txt"
	zipName                   = "cloudflared-diag"
	heapPprofBaseName         = "heap.pprof"
	goroutinePprofBaseName    = "goroutine.pprof"
	networkBaseName           = "network.json"
	rawNetworkBaseName        = "raw-network.txt"
	tunnelStateBaseName       = "tunnelstate.json"
	cliConfigurationBaseName  = "cli-configuration.json"
	configurationBaseName     = "configuration.json"
	taskResultBaseName        = "task-result.json"
)
