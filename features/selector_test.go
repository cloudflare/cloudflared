package features

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestUnmarshalFeaturesRecord(t *testing.T) {
	tests := []struct {
		record []byte
	}{
		{
			record: []byte(`{"pq":0}`),
		},
		{
			record: []byte(`{"pq":39}`),
		},
		{
			record: []byte(`{"pq":100}`),
		},
		{
			record: []byte(`{}`), // Unmarshal to default struct if key is not present
		},
		{
			record: []byte(`{"kyber":768}`), // Unmarshal to default struct if key is not present
		},
	}

	for _, test := range tests {
		var features featuresRecord
		err := json.Unmarshal(test.record, &features)
		require.NoError(t, err)
		require.Equal(t, featuresRecord{}, features)
	}
}

func TestStaticFeatures(t *testing.T) {
	pqMode := PostQuantumStrict
	selector := newTestSelector(t, &pqMode, time.Millisecond*10)
	require.Equal(t, PostQuantumStrict, selector.PostQuantumMode())

	// No StaticFeatures configured
	selector = newTestSelector(t, nil, time.Millisecond*10)
	require.Equal(t, PostQuantumPrefer, selector.PostQuantumMode())
}

func newTestSelector(t *testing.T, pqMode *PostQuantumMode, refreshFreq time.Duration) *FeatureSelector {
	accountTag := t.Name()
	logger := zerolog.Nop()

	resolver := &mockResolver{}

	staticFeatures := StaticFeatures{
		PostQuantumMode: pqMode,
	}
	selector, err := newFeatureSelector(context.Background(), accountTag, &logger, resolver, staticFeatures, refreshFreq)
	require.NoError(t, err)

	return selector
}

type mockResolver struct{}

func (mr *mockResolver) lookupRecord(ctx context.Context) ([]byte, error) {
	return nil, fmt.Errorf("mockResolver hasn't implement lookupRecord")
}
