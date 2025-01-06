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
		record             []byte
		expectedPercentage int32
	}{
		{
			record:             []byte(`{"dv3":0}`),
			expectedPercentage: 0,
		},
		{
			record:             []byte(`{"dv3":39}`),
			expectedPercentage: 39,
		},
		{
			record:             []byte(`{"dv3":100}`),
			expectedPercentage: 100,
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
		require.Equal(t, test.expectedPercentage, features.DatagramV3Percentage, test)
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
			expectedFeatures: Dedup(append(defaultFeatures, FeaturePostQuantum)),
			expectedVersion:  PostQuantumStrict,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &staticResolver{record: featuresRecord{}}
			selector, err := newFeatureSelector(context.Background(), test.name, &logger, resolver, []string{}, test.cli, time.Second)
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
			cli:              []string{FeatureDatagramV3},
			remote:           featuresRecord{},
			expectedFeatures: Dedup(append(defaultFeatures, FeatureDatagramV3)),
			expectedVersion:  FeatureDatagramV3,
		},
		{
			name: "remote_specified_v3",
			cli:  []string{},
			remote: featuresRecord{
				DatagramV3Percentage: 100,
			},
			expectedFeatures: Dedup(append(defaultFeatures, FeatureDatagramV3)),
			expectedVersion:  FeatureDatagramV3,
		},
		{
			name: "remote_and_user_specified_v3",
			cli:  []string{FeatureDatagramV3},
			remote: featuresRecord{
				DatagramV3Percentage: 100,
			},
			expectedFeatures: Dedup(append(defaultFeatures, FeatureDatagramV3)),
			expectedVersion:  FeatureDatagramV3,
		},
		{
			name: "remote_v3_and_user_specified_v2",
			cli:  []string{FeatureDatagramV2},
			remote: featuresRecord{
				DatagramV3Percentage: 100,
			},
			expectedFeatures: defaultFeatures,
			expectedVersion:  DatagramV2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &staticResolver{record: test.remote}
			selector, err := newFeatureSelector(context.Background(), test.name, &logger, resolver, test.cli, false, time.Second)
			require.NoError(t, err)
			require.ElementsMatch(t, test.expectedFeatures, selector.ClientFeatures())
			require.Equal(t, test.expectedVersion, selector.DatagramVersion())
		})
	}
}

func TestRefreshFeaturesRecord(t *testing.T) {
	// The hash of the accountTag is 82
	accountTag := t.Name()
	threshold := switchThreshold(accountTag)

	percentages := []int32{0, 10, 81, 82, 83, 100, 101, 1000}
	refreshFreq := time.Millisecond * 10
	selector := newTestSelector(t, percentages, false, refreshFreq)

	// Starting out should default to DatagramV2
	require.Equal(t, DatagramV2, selector.DatagramVersion())

	for _, percentage := range percentages {
		if percentage > threshold {
			require.Equal(t, DatagramV3, selector.DatagramVersion())
		} else {
			require.Equal(t, DatagramV2, selector.DatagramVersion())
		}

		time.Sleep(refreshFreq + time.Millisecond)
	}

	// Make sure error doesn't override the last fetched features
	require.Equal(t, DatagramV3, selector.DatagramVersion())
}

func TestStaticFeatures(t *testing.T) {
	percentages := []int32{0}
	// PostQuantum Enabled from user flag
	selector := newTestSelector(t, percentages, true, time.Millisecond*10)
	require.Equal(t, PostQuantumStrict, selector.PostQuantumMode())

	// PostQuantum Disabled (or not set)
	selector = newTestSelector(t, percentages, false, time.Millisecond*10)
	require.Equal(t, PostQuantumPrefer, selector.PostQuantumMode())
}

func newTestSelector(t *testing.T, percentages []int32, pq bool, refreshFreq time.Duration) *FeatureSelector {
	accountTag := t.Name()
	logger := zerolog.Nop()

	resolver := &mockResolver{
		percentages: percentages,
	}

	selector, err := newFeatureSelector(context.Background(), accountTag, &logger, resolver, []string{}, pq, refreshFreq)
	require.NoError(t, err)

	return selector
}

type mockResolver struct {
	nextIndex   int
	percentages []int32
}

func (mr *mockResolver) lookupRecord(ctx context.Context) ([]byte, error) {
	if mr.nextIndex >= len(mr.percentages) {
		return nil, fmt.Errorf("no more record to lookup")
	}

	record, err := json.Marshal(featuresRecord{
		DatagramV3Percentage: mr.percentages[mr.nextIndex],
	})
	mr.nextIndex++

	return record, err
}

type staticResolver struct {
	record featuresRecord
}

func (r *staticResolver) lookupRecord(ctx context.Context) ([]byte, error) {
	return json.Marshal(r.record)
}
