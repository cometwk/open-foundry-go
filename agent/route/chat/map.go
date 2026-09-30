package chat

import (
	"sync"
)

// SafeMap 泛型线程安全 Map 封装
type SafeMap[K comparable, V any] struct {
	mu sync.RWMutex
	m  map[K]V
}

func NewSafeMap[K comparable, V any]() *SafeMap[K, V] {
	return &SafeMap[K, V]{
		m: make(map[K]V),
	}
}

func (s *SafeMap[K, V]) Get(key K) (V, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	val, ok := s.m[key]
	return val, ok
}

func (s *SafeMap[K, V]) Set(key K, val V) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[key] = val
}

func (s *SafeMap[K, V]) Delete(key K) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.m, key)
}

// // GlobalCache 全局声明
// var GlobalCache = NewSafeMap[string, int]()

// func main() {
// 	GlobalCache.Set("user_id", 1001)
// 	if val, ok := GlobalCache.Get("user_id"); ok {
// 		fmt.Println("User ID:", val)
// 	}
// }
