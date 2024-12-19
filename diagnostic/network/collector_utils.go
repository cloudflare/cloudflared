package diagnostic

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
)

type DecodeLineFunc func(text string) (*Hop, error)

func decodeNetworkOutputToFile(command *exec.Cmd, decodeLine DecodeLineFunc) ([]*Hop, string, error) {
	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, "", fmt.Errorf("error piping traceroute's output: %w", err)
	}

	if err := command.Start(); err != nil {
		return nil, "", fmt.Errorf("error starting traceroute: %w", err)
	}

	// Tee the output to a string to have the raw information
	// in case the decode call fails
	// This error is handled only after the Wait call below returns
	// otherwise the process can become a zombie
	buf := bytes.NewBuffer([]byte{})
	tee := io.TeeReader(stdout, buf)
	hops, err := Decode(tee, decodeLine)
	// regardless of success of the decoding
	// consume all output to have available in buf
	_, _ = io.ReadAll(tee)

	if werr := command.Wait(); werr != nil {
		return nil, "", fmt.Errorf("error finishing traceroute: %w", werr)
	}

	if err != nil {
		return nil, buf.String(), err
	}

	return hops, buf.String(), nil
}

func Decode(reader io.Reader, decodeLine DecodeLineFunc) ([]*Hop, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Split(bufio.ScanLines)

	var hops []*Hop

	for scanner.Scan() {
		text := scanner.Text()
		if text == "" {
			continue
		}

		hop, err := decodeLine(text)
		if err != nil {
			// This continue is here on the error case because there are lines at the start and end
			// that may not be parsable. (check windows tracert output)
			// The skip is here because aside from the start and end lines the other lines should
			// always be parsable without errors.
			continue
		}

		hops = append(hops, hop)
	}

	if scanner.Err() != nil {
		return nil, fmt.Errorf("scanner reported an error: %w", scanner.Err())
	}

	return hops, nil
}
