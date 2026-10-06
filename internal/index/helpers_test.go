package index

func (idx *Indexer) IsIndexed() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return idx.indexed
}
