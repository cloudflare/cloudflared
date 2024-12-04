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
	"sync"
	"time"

	"github.com/rs/zerolog"

	network "github.com/cloudflare/cloudflared/diagnostic/network"
)

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
	if err != nil {
		if raw == "" {
			// An error happened and there is no raw output
			results <- networkCollectionResult{name, nil, "", err}
		} else {
			// An error happened and there is raw output then write to file
			results <- networkCollectionResult{name, nil, raw, nil}
		}
	} else {
		results <- networkCollectionResult{name, hops, raw, nil}
	}
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
		if result.err != nil {
			continue
		}

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

	for k, v := range resultMap {
		_, err := networkDumpHandle.WriteString(k + "\n" + v.raw + "\n")
		if err != nil {
			return "", fmt.Errorf("error writing raw network information: %w", err)
		}
	}

	return networkDumpHandle.Name(), nil
}

func jsonNetworkInformationWriter(resultMap map[string]networkCollectionResult) (string, error) {
	jsonMap := make(map[string][]*network.Hop, len(resultMap))
	for k, v := range resultMap {
		jsonMap[k] = v.info
	}

	networkDumpHandle, err := os.Create(filepath.Join(os.TempDir(), networkBaseName))
	if err != nil {
		return "", ErrCreatingTemporaryFile
	}

	defer networkDumpHandle.Close()

	err = json.NewEncoder(networkDumpHandle).Encode(jsonMap)
	if err != nil {
		return "", fmt.Errorf("error encoding network information results: %w", err)
	}

	return networkDumpHandle.Name(), nil
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
			return "", ErrCreatingTemporaryFile
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

		encoder := json.NewEncoder(writer)

		err := encoder.Encode(tunnel)

		return fmt.Errorf("error encoding tunnel state: %w", err)
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
		url, err := url.Parse(metricsServerAddress)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("provided address is not valid: %w", err)
		}

		if url.Scheme == "" {
			url.Scheme = "http://"
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
			jobName: "tunnel state",
			fn:      tunnelStateCollectEndpointAdapter(client, tunnel, tunnelStateBaseName),
			bypass:  false,
		},
		{
			jobName: "system information",
			fn:      collectFromEndpointAdapter(client.GetSystemInformation, systemInformationBaseName),
			bypass:  noDiagSystem,
		},
		{
			jobName: "goroutine profile",
			fn:      collectFromEndpointAdapter(client.GetGoroutineDump, goroutinePprofBaseName),
			bypass:  noDiagRuntime,
		},
		{
			jobName: "heap profile",
			fn:      collectFromEndpointAdapter(client.GetMemoryDump, heapPprofBaseName),
			bypass:  noDiagRuntime,
		},
		{
			jobName: "metrics",
			fn:      collectFromEndpointAdapter(client.GetMetrics, metricsBaseName),
			bypass:  noDiagMetrics,
		},
		{
			jobName: "log information",
			fn: func(ctx context.Context) (string, error) {
				return collectLogs(ctx, client, diagContainer, diagPod)
			},
			bypass: noDiagLogs,
		},
		{
			jobName: "raw network information",
			fn:      rawNetworkCollectorFunc,
			bypass:  noDiagNetwork,
		},
		{
			jobName: "network information",
			fn:      jsonNetworkCollectorFunc,
			bypass:  noDiagNetwork,
		},
		{
			jobName: "cli configuration",
			fn:      collectFromEndpointAdapter(client.GetCliConfiguration, cliConfigurationBaseName),
			bypass:  false,
		},
		{
			jobName: "configuration",
			fn:      collectFromEndpointAdapter(client.GetTunnelConfiguration, configurationBaseName),
			bypass:  false,
		},
	}

	return jobs
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

	paths := make([]string, 0)
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

	for _, job := range jobs {
		if job.bypass {
			continue
		}

		log.Info().Msgf("Collecting %s...", job.jobName)
		path, err := job.fn(ctx)

		defer func() {
			if !errors.Is(err, ErrCreatingTemporaryFile) {
				os.Remove(path)
			}
		}()

		if err != nil {
			return nil, err
		}

		log.Info().Msgf("Collected %s.", job.jobName)

		paths = append(paths, path)
	}

	zipfile, err := CreateDiagnosticZipFile(zipName, paths)
	if err != nil {
		if zipfile != "" {
			os.Remove(zipfile)
		}

		return nil, err
	}

	log.Info().Msgf("Diagnostic file written: %v", zipfile)

	return nil, nil
}
