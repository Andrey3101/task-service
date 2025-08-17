package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/go-redis/redis/v8"
	"go.uber.org/zap"
)

// CacheService сервис для работы с кэшем Redis
type CacheService struct {
	redis  *redis.Client
	logger *zap.Logger
}

// NewCacheService создает новый сервис кэширования
func NewCacheService(redis *redis.Client, logger *zap.Logger) *CacheService {
	return &CacheService{
		redis:  redis,
		logger: logger,
	}
}

// Set сохраняет значение в кэш
func (s *CacheService) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) error {
	jsonValue, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	err = s.redis.Set(ctx, key, jsonValue, expiration).Err()
	if err != nil {
		s.logger.Error("Failed to set cache value", zap.String("key", key), zap.Error(err))
		return fmt.Errorf("failed to set cache value: %w", err)
	}

	s.logger.Debug("Cache value set", zap.String("key", key), zap.Duration("expiration", expiration))
	return nil
}

// Get получает значение из кэша
func (s *CacheService) Get(ctx context.Context, key string, dest interface{}) error {
	value, err := s.redis.Get(ctx, key).Result()
	if err != nil {
		if err == redis.Nil {
			s.logger.Debug("Cache miss", zap.String("key", key))
			return fmt.Errorf("cache miss: %s", key)
		}
		s.logger.Error("Failed to get cache value", zap.String("key", key), zap.Error(err))
		return fmt.Errorf("failed to get cache value: %w", err)
	}

	if err := json.Unmarshal([]byte(value), dest); err != nil {
		s.logger.Error("Failed to unmarshal cache value", zap.String("key", key), zap.Error(err))
		return fmt.Errorf("failed to unmarshal cache value: %w", err)
	}

	s.logger.Debug("Cache hit", zap.String("key", key))
	return nil
}

// Delete удаляет значение из кэша
func (s *CacheService) Delete(ctx context.Context, key string) error {
	err := s.redis.Del(ctx, key).Err()
	if err != nil {
		s.logger.Error("Failed to delete cache value", zap.String("key", key), zap.Error(err))
		return fmt.Errorf("failed to delete cache value: %w", err)
	}

	s.logger.Debug("Cache value deleted", zap.String("key", key))
	return nil
}

// DeletePattern удаляет значения по паттерну
func (s *CacheService) DeletePattern(ctx context.Context, pattern string) error {
	keys, err := s.redis.Keys(ctx, pattern).Result()
	if err != nil {
		s.logger.Error("Failed to get keys by pattern", zap.String("pattern", pattern), zap.Error(err))
		return fmt.Errorf("failed to get keys by pattern: %w", err)
	}

	if len(keys) > 0 {
		err = s.redis.Del(ctx, keys...).Err()
		if err != nil {
			s.logger.Error("Failed to delete cache values by pattern", zap.String("pattern", pattern), zap.Error(err))
			return fmt.Errorf("failed to delete cache values by pattern: %w", err)
		}
	}

	s.logger.Debug("Cache values deleted by pattern", zap.String("pattern", pattern), zap.Int("count", len(keys)))
	return nil
}

// Exists проверяет существование ключа в кэше
func (s *CacheService) Exists(ctx context.Context, key string) (bool, error) {
	exists, err := s.redis.Exists(ctx, key).Result()
	if err != nil {
		s.logger.Error("Failed to check cache key existence", zap.String("key", key), zap.Error(err))
		return false, fmt.Errorf("failed to check cache key existence: %w", err)
	}

	return exists > 0, nil
}

// SetNX сохраняет значение только если ключ не существует
func (s *CacheService) SetNX(ctx context.Context, key string, value interface{}, expiration time.Duration) (bool, error) {
	jsonValue, err := json.Marshal(value)
	if err != nil {
		return false, fmt.Errorf("failed to marshal value: %w", err)
	}

	success, err := s.redis.SetNX(ctx, key, jsonValue, expiration).Result()
	if err != nil {
		s.logger.Error("Failed to set NX cache value", zap.String("key", key), zap.Error(err))
		return false, fmt.Errorf("failed to set NX cache value: %w", err)
	}

	if success {
		s.logger.Debug("Cache value set NX", zap.String("key", key), zap.Duration("expiration", expiration))
	} else {
		s.logger.Debug("Cache value not set NX (key exists)", zap.String("key", key))
	}

	return success, nil
}

// Increment увеличивает числовое значение в кэше
func (s *CacheService) Increment(ctx context.Context, key string) (int64, error) {
	value, err := s.redis.Incr(ctx, key).Result()
	if err != nil {
		s.logger.Error("Failed to increment cache value", zap.String("key", key), zap.Error(err))
		return 0, fmt.Errorf("failed to increment cache value: %w", err)
	}

	s.logger.Debug("Cache value incremented", zap.String("key", key), zap.Int64("value", value))
	return value, nil
}

// Expire устанавливает время жизни ключа
func (s *CacheService) Expire(ctx context.Context, key string, expiration time.Duration) error {
	success, err := s.redis.Expire(ctx, key, expiration).Result()
	if err != nil {
		s.logger.Error("Failed to set cache expiration", zap.String("key", key), zap.Error(err))
		return fmt.Errorf("failed to set cache expiration: %w", err)
	}

	if success {
		s.logger.Debug("Cache expiration set", zap.String("key", key), zap.Duration("expiration", expiration))
	} else {
		s.logger.Debug("Cache expiration not set (key doesn't exist)", zap.String("key", key))
	}

	return nil
}

// TTL получает оставшееся время жизни ключа
func (s *CacheService) TTL(ctx context.Context, key string) (time.Duration, error) {
	ttl, err := s.redis.TTL(ctx, key).Result()
	if err != nil {
		s.logger.Error("Failed to get cache TTL", zap.String("key", key), zap.Error(err))
		return 0, fmt.Errorf("failed to get cache TTL: %w", err)
	}

	return ttl, nil
}

// Flush очищает весь кэш
func (s *CacheService) Flush(ctx context.Context) error {
	err := s.redis.FlushDB(ctx).Err()
	if err != nil {
		s.logger.Error("Failed to flush cache", zap.Error(err))
		return fmt.Errorf("failed to flush cache: %w", err)
	}

	s.logger.Info("Cache flushed successfully")
	return nil
}

// GetStats получает статистику кэша
func (s *CacheService) GetStats(ctx context.Context) (map[string]interface{}, error) {
	info, err := s.redis.Info(ctx).Result()
	if err != nil {
		s.logger.Error("Failed to get cache stats", zap.Error(err))
		return nil, fmt.Errorf("failed to get cache stats: %w", err)
	}

	stats := make(map[string]interface{})
	// Парсинг INFO команды Redis
	// В реальности здесь будет более детальный парсинг
	stats["info"] = info

	return stats, nil
}

// CacheKey генерирует ключ кэша для различных сущностей
func (s *CacheService) CacheKey(entityType, entityID string) string {
	return fmt.Sprintf("task_service:%s:%s", entityType, entityID)
}

// CacheKeyWithUser генерирует ключ кэша с пользователем
func (s *CacheService) CacheKeyWithUser(entityType, entityID, userID string) string {
	return fmt.Sprintf("task_service:%s:%s:user:%s", entityType, entityID, userID)
}

// CacheKeyWithCompany генерирует ключ кэша с компанией
func (s *CacheService) CacheKeyWithCompany(entityType, entityID, companyID string) string {
	return fmt.Sprintf("task_service:%s:%s:company:%s", entityType, entityID, companyID)
}

// CacheKeyPattern генерирует паттерн ключа кэша
func (s *CacheService) CacheKeyPattern(entityType string) string {
	return fmt.Sprintf("task_service:%s:*", entityType)
}
