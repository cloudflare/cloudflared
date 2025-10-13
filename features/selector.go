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
	lookupTimeout           = time.Second * 10
	defaultLookupFreq       = time.Hour
)

// If the TXT record adds other fields, the umarshal logic will ignore those keys
// If the TXT record is missing a key, the field will unmarshal to the default Go value

type featuresRecord struct {
	DatagramV3Percentage uint32 `json:"dv3_2"`

	// DatagramV3Percentage int32 `json:"dv3"` // Removed in TUN-9291
	// DatagramV3Percentage uint32 `json:"dv3_1"` // Removed in TUN-9883
	// PostQuantumPercentage int32 `json:"pq"` // Removed in TUN-7970
}

func NewFeatureSelector(ctx context.Context, accountTag string, cliFeatures []string, pq bool, logger *zerolog.Logger) (FeatureSelector, error) {
	return newFeatureSelector(ctx, accountTag, logger, newDNSResolver(), cliFeatures, pq, defaultLookupFreq)
}

type FeatureSelector interface {
	Snapshot() FeatureSnapshot
}

// FeatureSelector determines if this account will try new features; loaded once during startup.
type featureSelector struct {
	accountHash uint32
	logger      *zerolog.Logger
	resolver    resolver

	staticFeatures staticFeatures
	cliFeatures    []string

	// lock protects concurrent access to dynamic features
	lock           sync.RWMutex
	remoteFeatures featuresRecord
}

func newFeatureSelector(ctx context.Context, accountTag string, logger *zerolog.Logger, resolver resolver, cliFeatures []string, pq bool, refreshFreq time.Duration) (*featureSelector, error) {
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
	selector := &featureSelector{
		accountHash:    switchThreshold(accountTag),
		logger:         logger,
		resolver:       resolver,
		staticFeatures: staticFeatures,
		cliFeatures:    dedupAndRemoveFeatures(cliFeatures),
	}

	// Load the remote features
	if err := selector.refresh(ctx); err != nil {
		logger.Err(err).Msg("Failed to fetch features, default to disable")
	}

	// Spin off reloading routine
	go selector.refreshLoop(ctx, refreshFreq)

	return selector, nil
}

func (fs *featureSelector) Snapshot() FeatureSnapshot {
	fs.lock.RLock()
	defer fs.lock.RUnlock()
	return FeatureSnapshot{
		PostQuantum:     fs.postQuantumMode(),
		DatagramVersion: fs.datagramVersion(),
		FeaturesList:    fs.clientFeatures(),
	}
}

func (fs *featureSelector) accountEnabled(percentage uint32) bool {
	return percentage > fs.accountHash
}

func (fs *featureSelector) postQuantumMode() PostQuantumMode {
	if fs.staticFeatures.PostQuantumMode != nil {
		return *fs.staticFeatures.PostQuantumMode
	}

	return PostQuantumPrefer
}

func (fs *featureSelector) datagramVersion() DatagramVersion {
	// If user provides the feature via the cli, we take it as priority over remote feature evaluation
	if slices.Contains(fs.cliFeatures, FeatureDatagramV3_2) {
		return DatagramV3
	}
	// If the user specifies DatagramV2, we also take that over remote
	if slices.Contains(fs.cliFeatures, FeatureDatagramV2) {
		return DatagramV2
	}

	if fs.accountEnabled(fs.remoteFeatures.DatagramV3Percentage) {
		return DatagramV3
	}

	return DatagramV2
}

// clientFeatures will return the list of currently available features that cloudflared should provide to the edge.
func (fs *featureSelector) clientFeatures() []string {
	// Evaluate any remote features along with static feature list to construct the list of features
	return dedupAndRemoveFeatures(slices.Concat(defaultFeatures, fs.cliFeatures, []string{string(fs.datagramVersion())}))
}

func (fs *featureSelector) refresh(ctx context.Context) error {
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

	fs.remoteFeatures = features

	return nil
}

func (fs *featureSelector) refreshLoop(ctx context.Context, refreshFreq time.Duration) {
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

func switchThreshold(accountTag string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(accountTag))
	return h.Sum32() % 100
}
