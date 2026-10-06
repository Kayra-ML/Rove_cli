package secrets

import "os"

func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.mem, id)
	err := os.Remove(s.blobPath(id))
	if os.IsNotExist(err) {
		return ErrNotFound
	}
	return err
}
