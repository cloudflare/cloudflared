package features

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net"
	"slices"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	featureSelectorHostname = "cfd-features.argotunnel.com"
	defaultRefreshFreq      = time.Hour * 6
	lookupTimeout           = time.Second * 10
)

// If the TXT record adds other fields, the umarshal logic will ignore those keys
// If the TXT record is missing a key, the field will unmarshal to the default Go value

type featuresRecord struct {
	// support_datagram_v3
	DatagramV3Percentage int32 `json:"dv3"`

	// PostQuantumPercentage int32 `json:"pq"` // Removed in TUN-7970
}

func NewFeatureSelector(ctx context.Context, accountTag string, cliFeatures []string, pq bool, logger *zerolog.Logger) (*FeatureSelector, error) {
	return newFeatureSelector(ctx, accountTag, logger, newDNSResolver(), cliFeatures, pq, defaultRefreshFreq)
}

// FeatureSelector determines if this account will try new features. It periodically queries a DNS TXT record
// to see which features are turned on.
type FeatureSelector struct {
	accountHash int32
	logger      *zerolog.Logger
	resolver    resolver

	staticFeatures staticFeatures
	cliFeatures    []string

	// lock protects concurrent access to dynamic features
	lock     sync.RWMutex
	features featuresRecord
}

func newFeatureSelector(ctx context.Context, accountTag string, logger *zerolog.Logger, resolver resolver, cliFeatures []string, pq bool, refreshFreq time.Duration) (*FeatureSelector, error) {
	// Combine default features and user-provided features
	var pqMode *PostQuantumMode
	if pq {
		mode := PostQuantumStrict
		pqMode = &mode
		cliFeatures = append(cliFeatures, FeaturePostQuantum)
	}
	staticFeatures := staticFeatures{
		PostQuantumMode: pqMode,
	}
	selector := &FeatureSelector{
		accountHash:    switchThreshold(accountTag),
		logger:         logger,
		resolver:       resolver,
		staticFeatures: staticFeatures,
		cliFeatures:    Dedup(cliFeatures),
	}

	if err := selector.refresh(ctx); err != nil {
		logger.Err(err).Msg("Failed to fetch features, default to disable")
	}

	go selector.refreshLoop(ctx, refreshFreq)

	return selector, nil
}

func (fs *FeatureSelector) accountEnabled(percentage int32) bool {
	return percentage > fs.accountHash
}

func (fs *FeatureSelector) PostQuantumMode() PostQuantumMode {
	if fs.staticFeatures.PostQuantumMode != nil {
		return *fs.staticFeatures.PostQuantumMode
	}

	return PostQuantumPrefer
}

func (fs *FeatureSelector) DatagramVersion() DatagramVersion {
	fs.lock.RLock()
	defer fs.lock.RUnlock()

	// If user provides the feature via the cli, we take it as priority over remote feature evaluation
	if slices.Contains(fs.cliFeatures, FeatureDatagramV3) {
		return DatagramV3
	}
	// If the user specifies DatagramV2, we also take that over remote
	if slices.Contains(fs.cliFeatures, FeatureDatagramV2) {
		return DatagramV2
	}

	if fs.accountEnabled(fs.features.DatagramV3Percentage) {
		return DatagramV3
	}
	return DatagramV2
}

// ClientFeatures will return the list of currently available features that cloudflared should provide to the edge.
//
// This list is dynamic and can change in-between returns.
func (fs *FeatureSelector) ClientFeatures() []string {
	// Evaluate any remote features along with static feature list to construct the list of features
	return Dedup(slices.Concat(defaultFeatures, fs.cliFeatures, []string{string(fs.DatagramVersion())}))
}

func (fs *FeatureSelector) refreshLoop(ctx context.Context, refreshFreq time.Duration) {
	ticker := time.NewTicker(refreshFreq)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			err := fs.refresh(ctx)
			if err != nil {
				fs.logger.Err(err).Msg("Failed to refresh feature selector")
			}
		}
	}
}

func (fs *FeatureSelector) refresh(ctx context.Context) error {
	record, err := fs.resolver.lookupRecord(ctx)
	if err != nil {
		return err
	}

	var features featuresRecord
	if err := json.Unmarshal(record, &features); err != nil {
		return err
	}

	fs.lock.Lock()
	defer fs.lock.Unlock()

	fs.features = features

	return nil
}

// resolver represents an object that can look up featuresRecord
type resolver interface {
	lookupRecord(ctx context.Context) ([]byte, error)
}

type dnsResolver struct {
	resolver *net.Resolver
}

func newDNSResolver() *dnsResolver {
	return &dnsResolver{
		resolver: net.DefaultResolver,
	}
}

func (dr *dnsResolver) lookupRecord(ctx context.Context) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, lookupTimeout)
	defer cancel()

	records, err := dr.resolver.LookupTXT(ctx, featureSelectorHostname)
	if err != nil {
		return nil, err
	}

	if len(records) == 0 {
		return nil, fmt.Errorf("No TXT record found for %s to determine which features to opt-in", featureSelectorHostname)
	}

	return []byte(records[0]), nil
}

func switchThreshold(accountTag string) int32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(accountTag))
	return int32(h.Sum32() % 100)
}
