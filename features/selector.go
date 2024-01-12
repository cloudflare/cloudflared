package features

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog"
)

const (
	featureSelectorHostname = "cfd-features.argotunnel.com"
	defaultRefreshFreq      = time.Hour * 6
	lookupTimeout           = time.Second * 10
)

type PostQuantumMode uint8

const (
	// Prefer post quantum, but fallback if connection cannot be established
	PostQuantumPrefer PostQuantumMode = iota
	// If the user passes the --post-quantum flag, we override
	// CurvePreferences to only support hybrid post-quantum key agreements.
	PostQuantumStrict
)

// If the TXT record adds other fields, the umarshal logic will ignore those keys
// If the TXT record is missing a key, the field will unmarshal to the default Go value
// pq was removed in TUN-7970
type featuresRecord struct{}

func NewFeatureSelector(ctx context.Context, accountTag string, staticFeatures StaticFeatures, logger *zerolog.Logger) (*FeatureSelector, error) {
	return newFeatureSelector(ctx, accountTag, logger, newDNSResolver(), staticFeatures, defaultRefreshFreq)
}

// FeatureSelector determines if this account will try new features. It preiodically queries a DNS TXT record
// to see which features are turned on
type FeatureSelector struct {
	accountHash int32
	logger      *zerolog.Logger
	resolver    resolver

	staticFeatures StaticFeatures

	// lock protects concurrent access to dynamic features
	lock     sync.RWMutex
	features featuresRecord
}

// Features set by user provided flags
type StaticFeatures struct {
	PostQuantumMode *PostQuantumMode
}

func newFeatureSelector(ctx context.Context, accountTag string, logger *zerolog.Logger, resolver resolver, staticFeatures StaticFeatures, refreshFreq time.Duration) (*FeatureSelector, error) {
	selector := &FeatureSelector{
		accountHash:    switchThreshold(accountTag),
		logger:         logger,
		resolver:       resolver,
		staticFeatures: staticFeatures,
	}

	if err := selector.refresh(ctx); err != nil {
		logger.Err(err).Msg("Failed to fetch features, default to disable")
	}

	// Run refreshLoop next time we have a new feature to rollout

	return selector, nil
}

func (fs *FeatureSelector) PostQuantumMode() PostQuantumMode {
	if fs.staticFeatures.PostQuantumMode != nil {
		return *fs.staticFeatures.PostQuantumMode
	}

	return PostQuantumPrefer
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
