package naming

type Matcher interface {
	// MatchMetaData used to match with provided metadata map
	MatchMetaData(metadataMap map[string]string) bool
}

type EtcdMatcher struct {
	MetadataMap map[string]string
}

func (m EtcdMatcher) MatchMetaData(metadataMap map[string]string) bool {
	if metadataMap == nil {
		// nolint gosimple
		// keep code clearly to be understand
		if len(m.MetadataMap) == 0 {
			return true
		}
		return false
	}
	for key, value := range m.MetadataMap {
		mv, ok := metadataMap[key]
		if !ok {
			return false
		}

		if mv != value {
			return false
		}
	}

	return true
}

