package diagnostic

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

// CreateDiagnosticZipFile create a zip file with the contents from the all
// files paths. The files will be written in the root of the zip file.
// In case of an error occurs after whilst writing to the zip file
// this will be removed.
func CreateDiagnosticZipFile(base string, paths []string) (zipFileName string, err error) {
	// Create a zip file with all files from paths added to the root
	suffix := time.Now().Format(time.RFC3339)
	zipFileName = base + "-" + suffix + ".zip"
	zipFileName = strings.ReplaceAll(zipFileName, ":", "-")

	archive, cerr := os.Create(zipFileName)
	if cerr != nil {
		return "", fmt.Errorf("error creating file %s: %w", zipFileName, cerr)
	}

	archiveWriter := zip.NewWriter(archive)

	defer func() {
		archiveWriter.Close()
		archive.Close()

		if err != nil {
			os.Remove(zipFileName)
		}
	}()

	for _, file := range paths {
		if file == "" {
			continue
		}

		var handle *os.File

		handle, err = os.Open(file)
		if err != nil {
			return "", fmt.Errorf("error opening file %s: %w", zipFileName, err)
		}

		defer handle.Close()

		// Keep the base only to not create sub directories in the
		// zip file.
		var writer io.Writer

		writer, err = archiveWriter.Create(filepath.Base(file))
		if err != nil {
			return "", fmt.Errorf("error creating archive writer from %s: %w", file, err)
		}

		if _, err = io.Copy(writer, handle); err != nil {
			return "", fmt.Errorf("error copying file %s: %w", file, err)
		}
	}

	zipFileName = archive.Name()
	return zipFileName, nil
}

type AddressableTunnelState struct {
	*TunnelState
	URL *url.URL
}

func findMetricsServerPredicate(tunnelID, connectorID uuid.UUID) func(state *TunnelState) bool {
	if tunnelID != uuid.Nil && connectorID != uuid.Nil {
		return func(state *TunnelState) bool {
			return state.ConnectorID == connectorID && state.TunnelID == tunnelID
		}
	} else if tunnelID == uuid.Nil && connectorID != uuid.Nil {
		return func(state *TunnelState) bool {
			return state.ConnectorID == connectorID
		}
	} else if tunnelID != uuid.Nil && connectorID == uuid.Nil {
		return func(state *TunnelState) bool {
			return state.TunnelID == tunnelID
		}
	}

	return func(*TunnelState) bool {
		return true
	}
}

// The FindMetricsServer will try to find the metrics server url.
// There are two possible error scenarios:
// 1. No instance is found which will only return ErrMetricsServerNotFound
// 2. Multiple instances are found which will return an array of state and ErrMultipleMetricsServerFound
// In case of success, only the state for the instance is returned.
func FindMetricsServer(
	log *zerolog.Logger,
	client *httpClient,
	addresses []string,
) (*AddressableTunnelState, []*AddressableTunnelState, error) {
	instances := make([]*AddressableTunnelState, 0)

	for _, address := range addresses {
		url, err := url.Parse("http://" + address)
		if err != nil {
			log.Debug().Err(err).Msgf("error parsing address %s", address)

			continue
		}

		client.SetBaseURL(url)

		state, err := client.GetTunnelState(context.Background())
		if err == nil {
			instances = append(instances, &AddressableTunnelState{state, url})
		} else {
			log.Debug().Err(err).Msgf("error getting tunnel state from address %s", address)
		}
	}

	if len(instances) == 0 {
		return nil, nil, ErrMetricsServerNotFound
	}

	if len(instances) == 1 {
		return instances[0], nil, nil
	}

	return nil, instances, ErrMultipleMetricsServerFound
}

// newFormattedEncoder return a JSON encoder with identation
func newFormattedEncoder(w io.Writer) *json.Encoder {
	encoder := json.NewEncoder(w)
	encoder.SetIndent("", "  ")
	return encoder
}
