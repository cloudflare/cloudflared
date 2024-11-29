package diagnostic

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os/exec"
)

type DecodeLineFunc func(text string) (*Hop, error)

func decodeNetworkOutputToFile(command *exec.Cmd, fn DecodeLineFunc) ([]*Hop, string, error) {
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
	hops, err := Decode(tee, fn)

	if werr := command.Wait(); werr != nil {
		return nil, "", fmt.Errorf("error finishing traceroute: %w", werr)
	}

	if err != nil {
		// consume all output to have available in buf
		io.ReadAll(tee)
		// This is already a TracerouteError no need to wrap it
		return nil, buf.String(), err
	}

	return hops, "", nil
}

func Decode(reader io.Reader, fn DecodeLineFunc) ([]*Hop, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Split(bufio.ScanLines)

	var hops []*Hop
	for scanner.Scan() {
		text := scanner.Text()
		hop, err := fn(text)
		if err != nil {
			return nil, fmt.Errorf("error decoding output line: %w", err)
		}

		hops = append(hops, hop)
	}

	if scanner.Err() != nil {
		return nil, fmt.Errorf("scanner reported an error: %w", scanner.Err())
	}

	return hops, nil
}
