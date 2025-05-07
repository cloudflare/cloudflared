package features

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

func TestUnmarshalFeaturesRecord(t *testing.T) {
	tests := []struct {
		record             []byte
		expectedPercentage uint32
	}{
		{
			record: []byte(`{}`), // Unmarshal to default struct if key is not present
		},
		{
			record: []byte(`{"kyber":768}`), // Unmarshal to default struct if key is not present
		},
		{
			record: []byte(`{"pq": 101,"dv3":100}`), // Expired keys don't unmarshal to anything
		},
	}

	for _, test := range tests {
		var features featuresRecord
		err := json.Unmarshal(test.record, &features)
		require.NoError(t, err)
	}
}

func TestFeaturePrecedenceEvaluationPostQuantum(t *testing.T) {
	logger := zerolog.Nop()
	tests := []struct {
		name             string
		cli              bool
		expectedFeatures []string
		expectedVersion  PostQuantumMode
	}{
		{
			name:             "default",
			cli:              false,
			expectedFeatures: defaultFeatures,
			expectedVersion:  PostQuantumPrefer,
		},
		{
			name:             "user_specified",
			cli:              true,
			expectedFeatures: dedupAndRemoveFeatures(append(defaultFeatures, FeaturePostQuantum)),
			expectedVersion:  PostQuantumStrict,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &staticResolver{record: featuresRecord{}}
			selector, err := newFeatureSelector(context.Background(), test.name, &logger, resolver, []string{}, test.cli)
			require.NoError(t, err)
			require.ElementsMatch(t, test.expectedFeatures, selector.ClientFeatures())
			require.Equal(t, test.expectedVersion, selector.PostQuantumMode())
		})
	}
}

func TestFeaturePrecedenceEvaluationDatagramVersion(t *testing.T) {
	logger := zerolog.Nop()
	tests := []struct {
		name             string
		cli              []string
		remote           featuresRecord
		expectedFeatures []string
		expectedVersion  DatagramVersion
	}{
		{
			name:             "default",
			cli:              []string{},
			remote:           featuresRecord{},
			expectedFeatures: defaultFeatures,
			expectedVersion:  DatagramV2,
		},
		{
			name:             "user_specified_v2",
			cli:              []string{FeatureDatagramV2},
			remote:           featuresRecord{},
			expectedFeatures: defaultFeatures,
			expectedVersion:  DatagramV2,
		},
		{
			name:             "user_specified_v3",
			cli:              []string{FeatureDatagramV3_1},
			remote:           featuresRecord{},
			expectedFeatures: dedupAndRemoveFeatures(append(defaultFeatures, FeatureDatagramV3_1)),
			expectedVersion:  FeatureDatagramV3_1,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &staticResolver{record: test.remote}
			selector, err := newFeatureSelector(context.Background(), test.name, &logger, resolver, test.cli, false)
			require.NoError(t, err)
			require.ElementsMatch(t, test.expectedFeatures, selector.ClientFeatures())
			require.Equal(t, test.expectedVersion, selector.DatagramVersion())
		})
	}
}

func TestDeprecatedFeaturesRemoved(t *testing.T) {
	logger := zerolog.Nop()
	tests := []struct {
		name             string
		cli              []string
		remote           featuresRecord
		expectedFeatures []string
	}{
		{
			name:             "no_removals",
			cli:              []string{},
			remote:           featuresRecord{},
			expectedFeatures: defaultFeatures,
		},
		{
			name:             "support_datagram_v3",
			cli:              []string{DeprecatedFeatureDatagramV3},
			remote:           featuresRecord{},
			expectedFeatures: defaultFeatures,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &staticResolver{record: test.remote}
			selector, err := newFeatureSelector(context.Background(), test.name, &logger, resolver, test.cli, false)
			require.NoError(t, err)
			require.ElementsMatch(t, test.expectedFeatures, selector.ClientFeatures())
		})
	}
}

func TestStaticFeatures(t *testing.T) {
	percentages := []uint32{0}
	// PostQuantum Enabled from user flag
	selector := newTestSelector(t, percentages, true)
	require.Equal(t, PostQuantumStrict, selector.PostQuantumMode())

	// PostQuantum Disabled (or not set)
	selector = newTestSelector(t, percentages, false)
	require.Equal(t, PostQuantumPrefer, selector.PostQuantumMode())
}

func newTestSelector(t *testing.T, percentages []uint32, pq bool) *FeatureSelector {
	accountTag := t.Name()
	logger := zerolog.Nop()

	selector, err := newFeatureSelector(context.Background(), accountTag, &logger, &staticResolver{}, []string{}, pq)
	require.NoError(t, err)

	return selector
}

type staticResolver struct {
	record featuresRecord
}

func (r *staticResolver) lookupRecord(ctx context.Context) ([]byte, error) {
	return json.Marshal(r.record)
}
