package agents

// loadFn parses a settings file into a nested map. Returns (nil, nil)
// if the file doesn't exist.
type loadFn func(path string) (map[string]any, error)

// saveFn serializes a nested map back to a settings file.
type saveFn func(path string, root map[string]any) error

// ensureArrayEntries reads a (possibly missing) settings file, walks to
// the nested key path, ensures the value is an array, and appends any
// entries from `values` not already present. Returns the list of
// entries added this call (for state recording).
func ensureArrayEntries(load loadFn, save saveFn, filePath string, keyPath []string, values []string) ([]string, error) {
	root, err := load(filePath)
	if err != nil {
		return nil, err
	}
	if root == nil {
		root = map[string]any{}
	}
	cur := root
	for i, k := range keyPath {
		if i == len(keyPath)-1 {
			break
		}
		next, ok := cur[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[k] = next
		}
		cur = next
	}
	lastKey := keyPath[len(keyPath)-1]
	existing, _ := cur[lastKey].([]any)
	existingSet := map[string]bool{}
	for _, e := range existing {
		if s, ok := e.(string); ok {
			existingSet[s] = true
		}
	}
	var added []string
	for _, v := range values {
		if !existingSet[v] {
			existing = append(existing, v)
			added = append(added, v)
		}
	}
	cur[lastKey] = existing
	if err := save(filePath, root); err != nil {
		return nil, err
	}
	return added, nil
}

// removeArrayEntries removes the given entries from the array at
// keyPath. Missing keyPath or missing entries are no-ops.
func removeArrayEntries(load loadFn, save saveFn, filePath string, keyPath []string, values []string) error {
	root, err := load(filePath)
	if err != nil {
		return err
	}
	if root == nil {
		return nil
	}
	cur := root
	for i, k := range keyPath {
		if i == len(keyPath)-1 {
			break
		}
		next, ok := cur[k].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	lastKey := keyPath[len(keyPath)-1]
	existing, ok := cur[lastKey].([]any)
	if !ok {
		return nil
	}
	remove := map[string]bool{}
	for _, v := range values {
		remove[v] = true
	}
	kept := existing[:0]
	for _, e := range existing {
		if s, ok := e.(string); ok && remove[s] {
			continue
		}
		kept = append(kept, e)
	}
	cur[lastKey] = kept
	return save(filePath, root)
}
