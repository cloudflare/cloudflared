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
		expectedErr        bool
	}{
		{
			record:             []byte(`{"pq":0}`),
			expectedPercentage: 0,
		},
		{
			record:             []byte(`{"pq":39}`),
			expectedPercentage: 39,
		},
		{
			record:             []byte(`{"pq":100}`),
			expectedPercentage: 100,
		},
		{
			record:             []byte(`{}`),
			expectedPercentage: 0, // Unmarshal to default struct if key is not present
		},
		{
			record:             []byte(`{"kyber":768}`),
			expectedPercentage: 0, // Unmarshal to default struct if key is not present
		},
		{
			record:      []byte(`{"pq":"kyber768"}`),
			expectedErr: true,
		},
	}

	for _, test := range tests {
		var features featuresRecord
		err := json.Unmarshal(test.record, &features)
		if test.expectedErr {
			require.Error(t, err, test)
		} else {
			require.NoError(t, err)
			require.Equal(t, test.expectedPercentage, features.PostQuantumPercentage, test)
		}
	}
}

func TestRefreshFeaturesRecord(t *testing.T) {
	// The hash of the accountTag is 82
	accountTag := t.Name()
	threshold := switchThreshold(accountTag)

	percentages := []int32{0, 10, 80, 83, 100}
	refreshFreq := time.Millisecond * 10
	selector := newTestSelector(t, percentages, nil, refreshFreq)

	for _, percentage := range percentages {
		if percentage > threshold {
			require.Equal(t, PostQuantumPrefer, selector.PostQuantumMode())
		} else {
			require.Equal(t, PostQuantumDisabled, selector.PostQuantumMode())
		}

		time.Sleep(refreshFreq + time.Millisecond)
	}

	// Make sure error doesn't override the last fetched features
	require.Equal(t, PostQuantumPrefer, selector.PostQuantumMode())
}

func TestStaticFeatures(t *testing.T) {
	percentages := []int32{0}
	pqMode := PostQuantumStrict
	selector := newTestSelector(t, percentages, &pqMode, time.Millisecond*10)
	require.Equal(t, PostQuantumStrict, selector.PostQuantumMode())
}

// Verify that if the first refresh fails, the selector will use default features
func TestFailedRefreshInitToDefault(t *testing.T) {
	selector := newTestSelector(t, []int32{}, nil, time.Second)
	require.Equal(t, featuresRecord{}, selector.features)
	require.Equal(t, PostQuantumDisabled, selector.PostQuantumMode())
}

func newTestSelector(t *testing.T, percentages []int32, pqMode *PostQuantumMode, refreshFreq time.Duration) *FeatureSelector {
	accountTag := t.Name()
	logger := zerolog.Nop()

	resolver := &mockResolver{
		percentages: percentages,
	}

	staticFeatures := StaticFeatures{
		PostQuantumMode: pqMode,
	}
	selector, err := newFeatureSelector(context.Background(), accountTag, &logger, resolver, staticFeatures, refreshFreq)
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
		PostQuantumPercentage: mr.percentages[mr.nextIndex],
	})
	mr.nextIndex++

	return record, err
}
