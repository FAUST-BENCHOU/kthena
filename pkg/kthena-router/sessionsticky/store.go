/*
Copyright The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package sessionsticky

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
	"k8s.io/klog/v2"

	networkingv1alpha1 "github.com/volcano-sh/kthena/pkg/apis/networking/v1alpha1"
)

// Store persists session key to pod name bindings with TTL.
type Store interface {
	Get(ctx context.Context, routeKey, sessionKey string) (podName string, ok bool, err error)
	Set(ctx context.Context, routeKey, sessionKey, podName string, ttl time.Duration) error
	Delete(ctx context.Context, routeKey, sessionKey string) error
}

func redisKey(routeKey, sessionKey string) string {
	h := sha256.Sum256([]byte(sessionKey))
	return fmt.Sprintf("kthena:sessionsticky:%s:%s", routeKey, hex.EncodeToString(h[:]))
}

// MemoryStore is a process-local session mapping (single replica or testing).
type MemoryStore struct {
	mu sync.RWMutex
	// routeKey -> sessionHash -> entry
	data map[string]map[string]memoryEntry
}

type memoryEntry struct {
	podName string
	expiry  time.Time
}

// NewMemoryStore creates an in-memory store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{data: make(map[string]map[string]memoryEntry)}
}

func (m *MemoryStore) Get(ctx context.Context, routeKey, sessionKey string) (string, bool, error) {
	_ = ctx
	sk := sessionHashKey(sessionKey)
	m.mu.RLock()
	defer m.mu.RUnlock()
	byRoute, ok := m.data[routeKey]
	if !ok {
		return "", false, nil
	}
	ent, ok := byRoute[sk]
	if !ok || time.Now().After(ent.expiry) {
		return "", false, nil
	}
	return ent.podName, true, nil
}

func (m *MemoryStore) Set(ctx context.Context, routeKey, sessionKey, podName string, ttl time.Duration) error {
	_ = ctx
	sk := sessionHashKey(sessionKey)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.data[routeKey] == nil {
		m.data[routeKey] = make(map[string]memoryEntry)
	}
	m.data[routeKey][sk] = memoryEntry{podName: podName, expiry: time.Now().Add(ttl)}
	return nil
}

func (m *MemoryStore) Delete(ctx context.Context, routeKey, sessionKey string) error {
	_ = ctx
	sk := sessionHashKey(sessionKey)
	m.mu.Lock()
	defer m.mu.Unlock()
	if byRoute, ok := m.data[routeKey]; ok {
		delete(byRoute, sk)
	}
	return nil
}

func sessionHashKey(sessionKey string) string {
	h := sha256.Sum256([]byte(sessionKey))
	return hex.EncodeToString(h[:])
}

// RedisStore uses Redis for shared mappings across gateway replicas.
type RedisStore struct {
	client *redis.Client
}

// NewRedisStore creates a Redis-backed store.
func NewRedisStore(client *redis.Client) *RedisStore {
	return &RedisStore{client: client}
}

func (r *RedisStore) Get(ctx context.Context, routeKey, sessionKey string) (string, bool, error) {
	key := redisKey(routeKey, sessionKey)
	val, err := r.client.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return val, true, nil
}

func (r *RedisStore) Set(ctx context.Context, routeKey, sessionKey, podName string, ttl time.Duration) error {
	key := redisKey(routeKey, sessionKey)
	return r.client.Set(ctx, key, podName, ttl).Err()
}

func (r *RedisStore) Delete(ctx context.Context, routeKey, sessionKey string) error {
	key := redisKey(routeKey, sessionKey)
	return r.client.Del(ctx, key).Err()
}

// Manager resolves Store for a ModelRoute session sticky configuration.
type Manager struct {
	memory *MemoryStore
	// address -> client
	redisMu      sync.Mutex
	redisClients map[string]*redis.Client
}

// NewManager creates a session sticky manager with a shared memory store.
func NewManager() *Manager {
	return &Manager{
		memory:       NewMemoryStore(),
		redisClients: make(map[string]*redis.Client),
	}
}

// StoreFor returns the Store for the given spec (defaults to Memory).
func (m *Manager) StoreFor(spec *networkingv1alpha1.SessionSticky) (Store, error) {
	if spec == nil {
		return m.memory, nil
	}
	backend := networkingv1alpha1.SessionStickyBackendMemory
	if spec.Backend != nil {
		backend = *spec.Backend
	}
	switch backend {
	case networkingv1alpha1.SessionStickyBackendRedis:
		if spec.Redis == nil || spec.Redis.Address == "" {
			return nil, fmt.Errorf("sessionSticky.redis.address is required when backend is Redis")
		}
		return m.getRedisStore(spec.Redis.Address)
	default:
		return m.memory, nil
	}
}

func (m *Manager) getRedisStore(address string) (Store, error) {
	m.redisMu.Lock()
	defer m.redisMu.Unlock()
	if c, ok := m.redisClients[address]; ok {
		return NewRedisStore(c), nil
	}
	c := redis.NewClient(&redis.Options{Addr: address})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := c.Ping(ctx).Err(); err != nil {
		_ = c.Close()
		return nil, fmt.Errorf("redis ping failed for session sticky: %w", err)
	}
	klog.InfoS("session sticky: connected to Redis", "address", address)
	m.redisClients[address] = c
	return NewRedisStore(c), nil
}

// Close releases Redis clients held by the manager.
func (m *Manager) Close() {
	m.redisMu.Lock()
	defer m.redisMu.Unlock()
	for addr, c := range m.redisClients {
		if err := c.Close(); err != nil {
			klog.ErrorS(err, "session sticky: close redis", "address", addr)
		}
		delete(m.redisClients, addr)
	}
}
