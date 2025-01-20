package diagnostic

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog"

	network "github.com/cloudflare/cloudflared/diagnostic/network"
)

const (
	taskSuccess                  = "success"
	taskFailure                  = "failure"
	jobReportName                = "job report"
	tunnelStateJobName           = "tunnel state"
	systemInformationJobName     = "system information"
	goroutineJobName             = "goroutine profile"
	heapJobName                  = "heap profile"
	metricsJobName               = "metrics"
	logInformationJobName        = "log information"
	rawNetworkInformationJobName = "raw network information"
	networkInformationJobName    = "network information"
	cliConfigurationJobName      = "cli configuration"
	configurationJobName         = "configuration"
)

// Struct used to hold the results of different routines executing the network collection.
type taskResult struct {
	Result string `json:"result,omitempty"`
	Err    error  `json:"error,omitempty"`
	path   string
}

func (result taskResult) MarshalJSON() ([]byte, error) {
	s := map[string]string{
		"result": result.Result,
	}
	if result.Err != nil {
		s["error"] = result.Err.Error()
	}

	return json.Marshal(s)
}

// Struct used to hold the results of different routines executing the network collection.
type networkCollectionResult struct {
	name string
	info []*network.Hop
	raw  string
	err  error
}

// This type represents the most common functions from the diagnostic http client
// functions.
type collectToWriterFunc func(ctx context.Context, writer io.Writer) error

// This type represents the common denominator among all the collection procedures.
type collectFunc func(ctx context.Context) (string, error)

// collectJob is an internal struct that denotes holds the information necessary
// to run a collection job.
type collectJob struct {
	jobName string
	fn      collectFunc
	bypass  bool
}

// The Toggles structure denotes the available toggles for the diagnostic procedure.
// Each toggle enables/disables tasks from the diagnostic.
type Toggles struct {
	NoDiagLogs    bool
	NoDiagMetrics bool
	NoDiagSystem  bool
	NoDiagRuntime bool
	NoDiagNetwork bool
}

// The Options structure holds every option necessary for
// the diagnostic procedure to work.
type Options struct {
	KnownAddresses []string
	Address        string
	ContainerID    string
	PodID          string
	Toggles        Toggles
}

func collectLogs(
	ctx context.Context,
	client HTTPClient,
	diagContainer, diagPod string,
) (string, error) {
	var collector LogCollector
	if diagPod != "" {
		collector = NewKubernetesLogCollector(diagContainer, diagPod)
	} else if diagContainer != "" {
		collector = NewDockerLogCollector(diagContainer)
	} else {
		collector = NewHostLogCollector(client)
	}

	logInformation, err := collector.Collect(ctx)
	if err != nil {
		return "", fmt.Errorf("error collecting logs: %w", err)
	}

	if logInformation.isDirectory {
		return CopyFilesFromDirectory(logInformation.path)
	}

	if logInformation.wasCreated {
		return logInformation.path, nil
	}

	logHandle, err := os.Open(logInformation.path)
	if err != nil {
		return "", fmt.Errorf("error opening log file while collecting logs: %w", err)
	}
	defer logHandle.Close()

	outputLogHandle, err := os.Create(filepath.Join(os.TempDir(), logFilename))
	if err != nil {
		return "", ErrCreatingTemporaryFile
	}
	defer outputLogHandle.Close()

	_, err = io.Copy(outputLogHandle, logHandle)
	if err != nil {
		return "", fmt.Errorf("error copying logs while collecting logs: %w", err)
	}

	return outputLogHandle.Name(), err
}

func collectNetworkResultRoutine(
	ctx context.Context,
	collector network.NetworkCollector,
	hostname string,
	useIPv4 bool,
	results chan networkCollectionResult,
) {
	const (
		hopsNo  = 5
		timeout = time.Second * 5
	)

	name := hostname

	if useIPv4 {
		name += "-v4"
	} else {
		name += "-v6"
	}

	hops, raw, err := collector.Collect(ctx, network.NewTraceOptions(hopsNo, timeout, hostname, useIPv4))
	results <- networkCollectionResult{name, hops, raw, err}
}

func gatherNetworkInformation(ctx context.Context) map[string]networkCollectionResult {
	networkCollector := network.NetworkCollectorImpl{}

	hostAndIPversionPairs := []struct {
		host  string
		useV4 bool
	}{
		{"region1.v2.argotunnel.com", true},
		{"region1.v2.argotunnel.com", false},
		{"region2.v2.argotunnel.com", true},
		{"region2.v2.argotunnel.com", false},
	}

	// the number of results is known thus use len to avoid footguns
	results := make(chan networkCollectionResult, len(hostAndIPversionPairs))

	var wgroup sync.WaitGroup

	for _, item := range hostAndIPversionPairs {
		wgroup.Add(1)

		go func() {
			defer wgroup.Done()
			collectNetworkResultRoutine(ctx, &networkCollector, item.host, item.useV4, results)
		}()
	}

	// Wait for routines to end.
	wgroup.Wait()

	resultMap := make(map[string]networkCollectionResult)

	for range len(hostAndIPversionPairs) {
		result := <-results
		resultMap[result.name] = result
	}

	return resultMap
}

func networkInformationCollectors() (rawNetworkCollector, jsonNetworkCollector collectFunc) {
	// The network collector is an operation that takes most of the diagnostic time, thus,
	// the sync.Once is used to memoize the result of the collector and then create different
	// outputs.
	var once sync.Once

	var resultMap map[string]networkCollectionResult

	rawNetworkCollector = func(ctx context.Context) (string, error) {
		once.Do(func() { resultMap = gatherNetworkInformation(ctx) })

		return rawNetworkInformationWriter(resultMap)
	}
	jsonNetworkCollector = func(ctx context.Context) (string, error) {
		once.Do(func() { resultMap = gatherNetworkInformation(ctx) })

		return jsonNetworkInformationWriter(resultMap)
	}

	return rawNetworkCollector, jsonNetworkCollector
}

func rawNetworkInformationWriter(resultMap map[string]networkCollectionResult) (string, error) {
	networkDumpHandle, err := os.Create(filepath.Join(os.TempDir(), rawNetworkBaseName))
	if err != nil {
		return "", ErrCreatingTemporaryFile
	}

	defer networkDumpHandle.Close()

	var exitErr error

	for k, v := range resultMap {
		if v.err != nil {
			if exitErr == nil {
				exitErr = v.err
			}

			_, err := networkDumpHandle.WriteString(k + "\nno content\n")
			if err != nil {
				return networkDumpHandle.Name(), fmt.Errorf("error writing 'no content' to raw network file: %w", err)
			}
		} else {
			_, err := networkDumpHandle.WriteString(k + "\n" + v.raw + "\n")
			if err != nil {
				return networkDumpHandle.Name(), fmt.Errorf("error writing raw network information: %w", err)
			}
		}
	}

	return networkDumpHandle.Name(), exitErr
}

func jsonNetworkInformationWriter(resultMap map[string]networkCollectionResult) (string, error) {
	networkDumpHandle, err := os.Create(filepath.Join(os.TempDir(), networkBaseName))
	if err != nil {
		return "", ErrCreatingTemporaryFile
	}

	defer networkDumpHandle.Close()

	encoder := newFormattedEncoder(networkDumpHandle)

	var exitErr error

	jsonMap := make(map[string][]*network.Hop, len(resultMap))
	for k, v := range resultMap {
		jsonMap[k] = v.info

		if exitErr == nil && v.err != nil {
			exitErr = v.err
		}
	}

	err = encoder.Encode(jsonMap)
	if err != nil {
		return networkDumpHandle.Name(), fmt.Errorf("error encoding network information results: %w", err)
	}

	return networkDumpHandle.Name(), exitErr
}

func collectFromEndpointAdapter(collect collectToWriterFunc, fileName string) collectFunc {
	return func(ctx context.Context) (string, error) {
		dumpHandle, err := os.Create(filepath.Join(os.TempDir(), fileName))
		if err != nil {
			return "", ErrCreatingTemporaryFile
		}
		defer dumpHandle.Close()

		err = collect(ctx, dumpHandle)
		if err != nil {
			return dumpHandle.Name(), fmt.Errorf("error running collector: %w", err)
		}

		return dumpHandle.Name(), nil
	}
}

func tunnelStateCollectEndpointAdapter(client HTTPClient, tunnel *TunnelState, fileName string) collectFunc {
	endpointFunc := func(ctx context.Context, writer io.Writer) error {
		if tunnel == nil {
			// When the metrics server is not passed the diagnostic will query all known hosts
			// and get the tunnel state, however, when the metrics server is passed that won't
			// happen hence the check for nil in this function.
			tunnelResponse, err := client.GetTunnelState(ctx)
			if err != nil {
				return fmt.Errorf("error retrieving tunnel state: %w", err)
			}

			tunnel = tunnelResponse
		}

		encoder := newFormattedEncoder(writer)

		err := encoder.Encode(tunnel)
		if err != nil {
			return fmt.Errorf("error encoding tunnel state: %w", err)
		}

		return nil
	}

	return collectFromEndpointAdapter(endpointFunc, fileName)
}

// resolveInstanceBaseURL is responsible to
// resolve the base URL of the instance that should be diagnosed.
// To resolve the instance it may be necessary to query the
// /diag/tunnel endpoint of the known instances, thus, if a single
// instance is found its state is also returned; if multiple instances
// are found then their states are returned in an array along with an
// error.
func resolveInstanceBaseURL(
	metricsServerAddress string,
	log *zerolog.Logger,
	client *httpClient,
	addresses []string,
) (*url.URL, *TunnelState, []*AddressableTunnelState, error) {
	if metricsServerAddress != "" {
		if !strings.HasPrefix(metricsServerAddress, "http://") {
			metricsServerAddress = "http://" + metricsServerAddress
		}
		url, err := url.Parse(metricsServerAddress)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("provided address is not valid: %w", err)
		}

		return url, nil, nil, nil
	}

	tunnelState, foundTunnelStates, err := FindMetricsServer(log, client, addresses)
	if err != nil {
		return nil, nil, foundTunnelStates, err
	}

	return tunnelState.URL, tunnelState.TunnelState, nil, nil
}

func createJobs(
	client *httpClient,
	tunnel *TunnelState,
	diagContainer string,
	diagPod string,
	noDiagSystem bool,
	noDiagRuntime bool,
	noDiagMetrics bool,
	noDiagLogs bool,
	noDiagNetwork bool,
) []collectJob {
	rawNetworkCollectorFunc, jsonNetworkCollectorFunc := networkInformationCollectors()
	jobs := []collectJob{
		{
			jobName: tunnelStateJobName,
			fn:      tunnelStateCollectEndpointAdapter(client, tunnel, tunnelStateBaseName),
			bypass:  false,
		},
		{
			jobName: systemInformationJobName,
			fn:      collectFromEndpointAdapter(client.GetSystemInformation, systemInformationBaseName),
			bypass:  noDiagSystem,
		},
		{
			jobName: goroutineJobName,
			fn:      collectFromEndpointAdapter(client.GetGoroutineDump, goroutinePprofBaseName),
			bypass:  noDiagRuntime,
		},
		{
			jobName: heapJobName,
			fn:      collectFromEndpointAdapter(client.GetMemoryDump, heapPprofBaseName),
			bypass:  noDiagRuntime,
		},
		{
			jobName: metricsJobName,
			fn:      collectFromEndpointAdapter(client.GetMetrics, metricsBaseName),
			bypass:  noDiagMetrics,
		},
		{
			jobName: logInformationJobName,
			fn: func(ctx context.Context) (string, error) {
				return collectLogs(ctx, client, diagContainer, diagPod)
			},
			bypass: noDiagLogs,
		},
		{
			jobName: rawNetworkInformationJobName,
			fn:      rawNetworkCollectorFunc,
			bypass:  noDiagNetwork,
		},
		{
			jobName: networkInformationJobName,
			fn:      jsonNetworkCollectorFunc,
			bypass:  noDiagNetwork,
		},
		{
			jobName: cliConfigurationJobName,
			fn:      collectFromEndpointAdapter(client.GetCliConfiguration, cliConfigurationBaseName),
			bypass:  false,
		},
		{
			jobName: configurationJobName,
			fn:      collectFromEndpointAdapter(client.GetTunnelConfiguration, configurationBaseName),
			bypass:  false,
		},
	}

	return jobs
}

func createTaskReport(taskReport map[string]taskResult) (string, error) {
	dumpHandle, err := os.Create(filepath.Join(os.TempDir(), taskResultBaseName))
	if err != nil {
		return "", ErrCreatingTemporaryFile
	}
	defer dumpHandle.Close()

	encoder := newFormattedEncoder(dumpHandle)

	err = encoder.Encode(taskReport)
	if err != nil {
		return "", fmt.Errorf("error encoding task results: %w", err)
	}

	return dumpHandle.Name(), nil
}

func runJobs(ctx context.Context, jobs []collectJob, log *zerolog.Logger) map[string]taskResult {
	jobReport := make(map[string]taskResult, len(jobs))

	for _, job := range jobs {
		if job.bypass {
			continue
		}

		log.Info().Msgf("Collecting %s...", job.jobName)
		path, err := job.fn(ctx)

		var result taskResult
		if err != nil {
			result = taskResult{Result: taskFailure, Err: err, path: path}

			log.Error().Err(err).Msgf("Job: %s finished with error.", job.jobName)
		} else {
			result = taskResult{Result: taskSuccess, Err: nil, path: path}

			log.Info().Msgf("Collected %s.", job.jobName)
		}

		jobReport[job.jobName] = result
	}

	taskReportName, err := createTaskReport(jobReport)

	var result taskResult

	if err != nil {
		result = taskResult{
			Result: taskFailure,
			path:   taskReportName,
			Err:    err,
		}
	} else {
		result = taskResult{
			Result: taskSuccess,
			path:   taskReportName,
			Err:    nil,
		}
	}

	jobReport[jobReportName] = result

	return jobReport
}

func RunDiagnostic(
	log *zerolog.Logger,
	options Options,
) ([]*AddressableTunnelState, error) {
	client := NewHTTPClient()

	baseURL, tunnel, foundTunnels, err := resolveInstanceBaseURL(options.Address, log, client, options.KnownAddresses)
	if err != nil {
		return foundTunnels, err
	}

	log.Info().Msgf("Selected server %s starting diagnostic...", baseURL.String())
	client.SetBaseURL(baseURL)

	const timeout = 45 * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	defer cancel()

	jobs := createJobs(
		client,
		tunnel,
		options.ContainerID,
		options.PodID,
		options.Toggles.NoDiagSystem,
		options.Toggles.NoDiagRuntime,
		options.Toggles.NoDiagMetrics,
		options.Toggles.NoDiagLogs,
		options.Toggles.NoDiagNetwork,
	)

	jobsReport := runJobs(ctx, jobs, log)
	paths := make([]string, 0)

	var gerr error

	for _, v := range jobsReport {
		paths = append(paths, v.path)

		if gerr == nil && v.Err != nil {
			gerr = v.Err
		}

		defer func() {
			if !errors.Is(v.Err, ErrCreatingTemporaryFile) {
				os.Remove(v.path)
			}
		}()
	}

	zipfile, err := CreateDiagnosticZipFile(zipName, paths)
	if err != nil {
		return nil, err
	}

	log.Info().Msgf("Diagnostic file written: %v", zipfile)

	return nil, gerr
}
