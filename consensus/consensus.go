package consensus

// StatePersistor is used to store consensus state which should survive a process crash
type StatePersistor interface {
	StoreState(key string, value []byte) error
	ReadState(key string) ([]byte, error)
	ReadStateSet(prefix string) (map[string][]byte, error)
	DelState(key string)
}
