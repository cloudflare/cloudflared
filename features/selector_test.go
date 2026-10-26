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

const (
	testAccountTag  = "123456"
	testAccountHash = 74 // switchThreshold of `accountTag`
)

func TestUnmarshalFeaturesRecord(t *testing.T) {
	tests := []struct {
		record             []byte
		expectedPercentage uint32
	}{
		{
			record:             []byte(`{"dv3_2":0}`),
			expectedPercentage: 0,
		},
		{
			record:             []byte(`{"dv3_2":39}`),
			expectedPercentage: 39,
		},
		{
			record:             []byte(`{"dv3_2":100}`),
			expectedPercentage: 100,
		},
		{
			record: []byte(`{}`), // Unmarshal to default struct if key is not present
		},
		{
			record: []byte(`{"kyber":768}`), // Unmarshal to default struct if key is not present
		},
		{
			record: []byte(`{"pq": 101,"dv3":100,"dv3_1":100}`), // Expired keys don't unmarshal to anything
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
			expectedFeatures: dedupAndRemoveFeatures(append(defaultFeatures, FeaturePostQuantum)),
			expectedVersion:  PostQuantumStrict,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &staticResolver{record: featuresRecord{}}
			selector, err := newFeatureSelector(t.Context(), test.name, &logger, resolver, []string{}, test.cli, time.Second)
			require.NoError(t, err)
			snapshot := selector.Snapshot()
			require.ElementsMatch(t, test.expectedFeatures, snapshot.FeaturesList)
			require.Equal(t, test.expectedVersion, snapshot.PostQuantum)
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
			cli:              []string{FeatureDatagramV3_2},
			remote:           featuresRecord{},
			expectedFeatures: dedupAndRemoveFeatures(append(defaultFeatures, FeatureDatagramV3_2)),
			expectedVersion:  FeatureDatagramV3_2,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &staticResolver{record: test.remote}
			selector, err := newFeatureSelector(t.Context(), test.name, &logger, resolver, test.cli, false, time.Second)
			require.NoError(t, err)
			snapshot := selector.Snapshot()
			require.ElementsMatch(t, test.expectedFeatures, snapshot.FeaturesList)
			require.Equal(t, test.expectedVersion, snapshot.DatagramVersion)
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
		{
			name:             "support_datagram_v3_1",
			cli:              []string{DeprecatedFeatureDatagramV3_1},
			remote:           featuresRecord{},
			expectedFeatures: defaultFeatures,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &staticResolver{record: test.remote}
			selector, err := newFeatureSelector(t.Context(), test.name, &logger, resolver, test.cli, false, time.Second)
			require.NoError(t, err)
			snapshot := selector.Snapshot()
			require.ElementsMatch(t, test.expectedFeatures, snapshot.FeaturesList)
		})
	}
}

func TestRefreshFeaturesRecord(t *testing.T) {
	percentages := []uint32{0, 10, testAccountHash - 1, testAccountHash, testAccountHash + 1, 100, 101, 1000}
	selector := newTestSelector(t, percentages, false, time.Minute)

	// Starting out should default to DatagramV2
	snapshot := selector.Snapshot()
	require.Equal(t, DatagramV2, snapshot.DatagramVersion)

	for _, percentage := range percentages {
		snapshot = selector.Snapshot()
		if percentage > testAccountHash {
			require.Equal(t, DatagramV3, snapshot.DatagramVersion)
		} else {
			require.Equal(t, DatagramV2, snapshot.DatagramVersion)
		}

		// Manually progress the next refresh
		_ = selector.refresh(t.Context())
	}

	// Make sure a resolver error doesn't override the last fetched features
	snapshot = selector.Snapshot()
	require.Equal(t, DatagramV3, snapshot.DatagramVersion)
}

func TestSnapshotIsolation(t *testing.T) {
	percentages := []uint32{testAccountHash, testAccountHash + 1}
	selector := newTestSelector(t, percentages, false, time.Minute)

	// Starting out should default to DatagramV2
	snapshot := selector.Snapshot()
	require.Equal(t, DatagramV2, snapshot.DatagramVersion)

	// Manually progress the next refresh
	_ = selector.refresh(t.Context())

	snapshot2 := selector.Snapshot()
	require.Equal(t, DatagramV3, snapshot2.DatagramVersion)
	require.NotEqual(t, snapshot.DatagramVersion, snapshot2.DatagramVersion)
}

func TestStaticFeatures(t *testing.T) {
	percentages := []uint32{0}
	// PostQuantum Enabled from user flag
	selector := newTestSelector(t, percentages, true, time.Second)
	snapshot := selector.Snapshot()
	require.Equal(t, PostQuantumStrict, snapshot.PostQuantum)

	// PostQuantum Disabled (or not set)
	selector = newTestSelector(t, percentages, false, time.Second)
	snapshot = selector.Snapshot()
	require.Equal(t, PostQuantumPrefer, snapshot.PostQuantum)
}

func newTestSelector(t *testing.T, percentages []uint32, pq bool, refreshFreq time.Duration) *featureSelector {
	logger := zerolog.Nop()

	resolver := &mockResolver{
		percentages: percentages,
	}

	selector, err := newFeatureSelector(t.Context(), testAccountTag, &logger, resolver, []string{}, pq, refreshFreq)
	require.NoError(t, err)

	return selector
}

type mockResolver struct {
	nextIndex   int
	percentages []uint32
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
