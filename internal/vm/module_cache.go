package vm

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"noxy-vm/internal/value"
)

type moduleKey struct {
	Root string
	Name string
}

type moduleEntry struct {
	done   chan struct{}
	result value.Value
	err    error
}

type moduleCache struct {
	mu           sync.Mutex
	entries      map[moduleKey]*moduleEntry
	dependencies map[moduleKey]map[moduleKey]struct{}
}

func newModuleCache() *moduleCache {
	return &moduleCache{
		entries:      make(map[moduleKey]*moduleEntry),
		dependencies: make(map[moduleKey]map[moduleKey]struct{}),
	}
}

func (cache *moduleCache) get(key moduleKey) (value.Value, bool) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, exists := cache.entries[key]
	if !exists {
		return value.NewNull(), false
	}
	select {
	case <-entry.done:
		return entry.result, entry.err == nil
	default:
		return value.NewNull(), false
	}
}

func (cache *moduleCache) store(key moduleKey, result value.Value) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry := &moduleEntry{done: make(chan struct{}), result: result}
	close(entry.done)
	cache.entries[key] = entry
}

func (cache *moduleCache) Do(key moduleKey, parent *moduleKey, load func() (value.Value, error)) (value.Value, error) {
	cache.mu.Lock()
	entry, exists := cache.entries[key]
	if exists {
		select {
		case <-entry.done:
			result, err := entry.result, entry.err
			cache.mu.Unlock()
			return result, err
		default:
		}
	}

	edgeAdded := false
	if parent != nil {
		if path, found := cache.pathLocked(key, *parent); found {
			cycle := append(path, key)
			cache.mu.Unlock()
			return value.NewNull(), fmt.Errorf("module import cycle: %s", formatModulePath(cycle))
		}
		children := cache.dependencies[*parent]
		if children == nil {
			children = make(map[moduleKey]struct{})
			cache.dependencies[*parent] = children
		}
		if _, exists := children[key]; !exists {
			children[key] = struct{}{}
			edgeAdded = true
		}
	}

	owner := !exists
	if owner {
		entry = &moduleEntry{done: make(chan struct{})}
		cache.entries[key] = entry
	}
	cache.mu.Unlock()

	if edgeAdded {
		defer cache.removeDependency(*parent, key)
	}

	if !owner {
		<-entry.done
		return entry.result, entry.err
	}

	result, err := load()
	cache.mu.Lock()
	entry.result = result
	entry.err = err
	if err != nil {
		delete(cache.entries, key)
	}
	close(entry.done)
	cache.mu.Unlock()
	return result, err
}

func (cache *moduleCache) pathLocked(from, to moduleKey) ([]moduleKey, bool) {
	path := make([]moduleKey, 0, 4)
	visited := make(map[moduleKey]struct{})
	var visit func(moduleKey) bool
	visit = func(current moduleKey) bool {
		if _, seen := visited[current]; seen {
			return false
		}
		visited[current] = struct{}{}
		path = append(path, current)
		if current == to {
			return true
		}

		children := make([]moduleKey, 0, len(cache.dependencies[current]))
		for child := range cache.dependencies[current] {
			children = append(children, child)
		}
		sort.Slice(children, func(i, j int) bool {
			if children[i].Root == children[j].Root {
				return children[i].Name < children[j].Name
			}
			return children[i].Root < children[j].Root
		})
		for _, child := range children {
			if visit(child) {
				return true
			}
		}
		path = path[:len(path)-1]
		return false
	}

	if !visit(from) {
		return nil, false
	}
	return append([]moduleKey(nil), path...), true
}

func (cache *moduleCache) removeDependency(parent, child moduleKey) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	children := cache.dependencies[parent]
	delete(children, child)
	if len(children) == 0 {
		delete(cache.dependencies, parent)
	}
}

func formatModulePath(path []moduleKey) string {
	names := make([]string, len(path))
	for i, key := range path {
		names[i] = key.Name
	}
	return strings.Join(names, " -> ")
}
