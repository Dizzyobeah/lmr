package ollama

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

// ModelCache provides cached access to available Ollama models.
type ModelCache struct {
	client          *Client
	models          []Model
	modelsByName    map[string]*Model
	mu              sync.RWMutex
	refreshInterval time.Duration
	lastRefresh     time.Time
	stopChan        chan struct{}
}

// NewModelCache creates a new model cache.
func NewModelCache(client *Client, refreshInterval time.Duration) *ModelCache {
	return &ModelCache{
		client:          client,
		modelsByName:    make(map[string]*Model),
		refreshInterval: refreshInterval,
		stopChan:        make(chan struct{}),
	}
}

// SetModels replaces the cached models. It is primarily intended for tests and
// for seeding the cache without a live Ollama client.
func (mc *ModelCache) SetModels(models []Model) {
	mc.mu.Lock()
	defer mc.mu.Unlock()

	mc.models = models
	mc.modelsByName = make(map[string]*Model, len(models))
	for i := range models {
		mc.modelsByName[models[i].Name] = &models[i]
	}
	mc.lastRefresh = time.Now()
}

// Start begins the background refresh loop.
func (mc *ModelCache) Start(ctx context.Context) error {
	// Initial refresh
	if err := mc.Refresh(ctx); err != nil {
		log.Warn().Err(err).Msg("Initial model refresh failed, will retry")
	}
	
	// Start background refresh
	go mc.refreshLoop()
	
	return nil
}

// Stop stops the background refresh loop.
func (mc *ModelCache) Stop() {
	close(mc.stopChan)
}

// refreshLoop periodically refreshes the model list.
func (mc *ModelCache) refreshLoop() {
	ticker := time.NewTicker(mc.refreshInterval)
	defer ticker.Stop()
	
	for {
		select {
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			if err := mc.Refresh(ctx); err != nil {
				log.Warn().Err(err).Msg("Failed to refresh model list")
			}
			cancel()
		case <-mc.stopChan:
			return
		}
	}
}

// Refresh updates the cached model list.
func (mc *ModelCache) Refresh(ctx context.Context) error {
	models, err := mc.client.ListModels(ctx)
	if err != nil {
		return err
	}
	
	mc.mu.Lock()
	defer mc.mu.Unlock()
	
	mc.models = models
	mc.modelsByName = make(map[string]*Model, len(models))
	for i := range models {
		mc.modelsByName[models[i].Name] = &models[i]
	}
	mc.lastRefresh = time.Now()
	
	log.Info().Int("count", len(models)).Msg("Model cache refreshed")
	
	return nil
}

// GetModels returns all cached models.
func (mc *ModelCache) GetModels() []Model {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	result := make([]Model, len(mc.models))
	copy(result, mc.models)
	return result
}

// GetModel returns a specific model by name.
func (mc *ModelCache) GetModel(name string) (*Model, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	model, ok := mc.modelsByName[name]
	if !ok {
		return nil, false
	}
	
	// Return a copy
	modelCopy := *model
	return &modelCopy, true
}

// HasModel checks if a model is available.
func (mc *ModelCache) HasModel(name string) bool {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	_, ok := mc.modelsByName[name]
	return ok
}

// GetModelNames returns the names of all available models.
func (mc *ModelCache) GetModelNames() []string {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	names := make([]string, len(mc.models))
	for i, m := range mc.models {
		names[i] = m.Name
	}
	return names
}

// FindFirstAvailable returns the first available model from the given list.
func (mc *ModelCache) FindFirstAvailable(preferred []string) (string, bool) {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	
	for _, name := range preferred {
		if _, ok := mc.modelsByName[name]; ok {
			return name, true
		}
	}
	return "", false
}

// LastRefresh returns when the cache was last refreshed.
func (mc *ModelCache) LastRefresh() time.Time {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return mc.lastRefresh
}

// Count returns the number of cached models.
func (mc *ModelCache) Count() int {
	mc.mu.RLock()
	defer mc.mu.RUnlock()
	return len(mc.models)
}
